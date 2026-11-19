package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// 网站目录和证书目录不再按发行版猜测，而是读 nginx 自己的主配置：
// Debian/Ubuntu 和 CentOS 系在 http 块里引用 conf.d/*.conf，Alpine 引用 http.d/*.conf
// （它的 conf.d 被引用在最外层，放 server 会报错），Homebrew 引用 servers/*，
// Arch、源码编译和 OpenResty 默认什么目录都不引用。

// 旧版本在每台机器的 config.toml 里都写入了这两个默认值，它们不代表用户的选择
const (
	legacySitesDir = "/etc/nginx/conf.d"
	legacySSLDir   = "/etc/nginx/ssl"
)

// hostOS 是当前系统，测试中可以替换
var hostOS = runtime.GOOS

// confDirective is a directive of an nginx configuration file; block holds the
// directives inside its braces.
type confDirective struct {
	name    string
	args    []string
	isBlock bool
	block   []confDirective
	start   int // offset of the directive name
	end     int // offset of the closing brace of the block
}

type confToken struct {
	text   string
	quoted bool
	pos    int
}

func lineOf(src string, pos int) int {
	return strings.Count(src[:pos], "\n") + 1
}

// tokenizeConf splits configuration text into words and the characters
// { } ; the way nginx reads it: # starts a comment only where a word could
// start, quotes group a word, and the { of ${var} belongs to the word.
func tokenizeConf(src string) ([]confToken, error) {
	var tokens []confToken
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '{' || c == '}' || c == ';':
			tokens = append(tokens, confToken{text: string(c), pos: i})
			i++
		case c == '"' || c == '\'':
			start := i
			var word strings.Builder
			for i++; ; {
				if i >= len(src) {
					return nil, fmt.Errorf("第 %d 行的引号没有闭合", lineOf(src, start))
				}
				if src[i] == '\\' && i+1 < len(src) {
					word.WriteByte(src[i+1])
					i += 2
					continue
				}
				if src[i] == c {
					i++
					break
				}
				word.WriteByte(src[i])
				i++
			}
			tokens = append(tokens, confToken{text: word.String(), quoted: true, pos: start})
		default:
			start := i
			var word strings.Builder
			for i < len(src) {
				ch := src[i]
				if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == ';' {
					break
				}
				if ch == '{' && (i == start || src[i-1] != '$') {
					break
				}
				if ch == '\\' && i+1 < len(src) {
					word.WriteByte(src[i+1])
					i += 2
					continue
				}
				word.WriteByte(ch)
				i++
			}
			tokens = append(tokens, confToken{text: word.String(), pos: start})
		}
	}
	return tokens, nil
}

type confParser struct {
	src    string
	tokens []confToken
	next   int
}

// parseConf parses nginx configuration text into its directives.
func parseConf(src string) ([]confDirective, error) {
	tokens, err := tokenizeConf(src)
	if err != nil {
		return nil, err
	}
	p := &confParser{src: src, tokens: tokens}
	directives, _, err := p.block(false)
	return directives, err
}

// block reads directives up to the closing brace of a block (nested) or the
// end of the file, and returns the offset of that brace.
func (p *confParser) block(nested bool) ([]confDirective, int, error) {
	var directives []confDirective
	var cur *confDirective
	for p.next < len(p.tokens) {
		t := p.tokens[p.next]
		p.next++
		if !t.quoted {
			switch t.text {
			case ";":
				if cur == nil {
					return nil, 0, fmt.Errorf("第 %d 行有多余的分号", lineOf(p.src, t.pos))
				}
				directives = append(directives, *cur)
				cur = nil
				continue
			case "{":
				if cur == nil {
					return nil, 0, fmt.Errorf("第 %d 行有多余的 {", lineOf(p.src, t.pos))
				}
				children, end, err := p.block(true)
				if err != nil {
					return nil, 0, err
				}
				cur.isBlock, cur.block, cur.end = true, children, end
				directives = append(directives, *cur)
				cur = nil
				continue
			case "}":
				if cur != nil {
					return nil, 0, fmt.Errorf("第 %d 行的 %s 缺少分号", lineOf(p.src, cur.start), cur.name)
				}
				if !nested {
					return nil, 0, fmt.Errorf("第 %d 行有多余的 }", lineOf(p.src, t.pos))
				}
				return directives, t.pos, nil
			}
		}
		if cur == nil {
			cur = &confDirective{name: t.text, start: t.pos}
		} else {
			cur.args = append(cur.args, t.text)
		}
	}
	if cur != nil {
		return nil, 0, fmt.Errorf("第 %d 行的 %s 缺少分号", lineOf(p.src, cur.start), cur.name)
	}
	if nested {
		return nil, 0, errors.New("文件结束时还有没闭合的 {")
	}
	return directives, len(p.src), nil
}

// httpBlock returns the http block of a main configuration file.
func httpBlock(directives []confDirective) *confDirective {
	for i := range directives {
		if directives[i].name == "http" && directives[i].isBlock {
			return &directives[i]
		}
	}
	return nil
}

// pidDirective returns the argument of the top-level pid directive.
func pidDirective(directives []confDirective) string {
	for _, d := range directives {
		if d.name == "pid" && !d.isBlock && len(d.args) > 0 {
			return d.args[0]
		}
	}
	return ""
}

// includedSiteDirs returns the directories whose *.conf files nginx loads in
// its http block, in the order nginx reads them. Relative include paths are
// relative to the directory of the main file, as in nginx.
func includedSiteDirs(src, confDir string) ([]string, error) {
	directives, err := parseConf(src)
	if err != nil {
		return nil, err
	}
	var dirs []string
	if http := httpBlock(directives); http != nil {
		collectSiteDirs(http.block, confDir, 0, &dirs)
	}
	return dirs, nil
}

func collectSiteDirs(block []confDirective, confDir string, depth int, dirs *[]string) {
	for _, d := range block {
		if d.name != "include" || d.isBlock || len(d.args) != 1 {
			continue
		}
		pattern := d.args[0]
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(confDir, pattern)
		}
		dir, glob := filepath.Split(pattern)
		dir = filepath.Clean(dir)
		if strings.ContainsAny(dir, "*?[") {
			continue
		}
		if !strings.ContainsAny(glob, "*?[") {
			// 单个文件（比如 vhosts.conf）里的 include 同样在 http 块中，最多跟进三层
			if depth < 3 {
				if src, err := os.ReadFile(pattern); err == nil {
					if nested, err := parseConf(string(src)); err == nil {
						collectSiteDirs(nested, confDir, depth+1, dirs)
					}
				}
			}
			continue
		}
		// 只认 conf.d/*.conf、sites-enabled/*、servers/* 这种能匹配到 example.com.conf 的通配符
		if ok, _ := filepath.Match(glob, "example.com.conf"); ok && !slices.Contains(*dirs, dir) {
			*dirs = append(*dirs, dir)
		}
	}
}

// AddSitesInclude returns the main configuration with `include <dir>/*.conf;`
// added as the last directive of its http block.
func AddSitesInclude(src, dir string) (string, error) {
	directives, err := parseConf(src)
	if err != nil {
		return "", fmt.Errorf("无法解析 nginx 主配置：%w", err)
	}
	http := httpBlock(directives)
	if http == nil {
		return "", errors.New("nginx 主配置里没有 http 块，请手动在 http 块中添加 include")
	}

	// 和 http 块里最后一条指令保持同样的缩进
	indent := "    "
	if n := len(http.block); n > 0 {
		lineStart := strings.LastIndexByte(src[:http.block[n-1].start], '\n') + 1
		if prefix := src[lineStart:http.block[n-1].start]; strings.Trim(prefix, " \t") == "" {
			indent = prefix
		}
	}
	pattern := filepath.Join(dir, "*.conf")
	if strings.ContainsAny(pattern, " \t;{}#'\"\\") {
		pattern = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(pattern) + `"`
	}
	line := indent + "include " + pattern + ";\n"

	// 插在 http 块的右括号所在行之前；右括号前面还有别的内容时另起一行
	lineStart := strings.LastIndexByte(src[:http.end], '\n') + 1
	if strings.Trim(src[lineStart:http.end], " \t") == "" {
		return src[:lineStart] + line + src[lineStart:], nil
	}
	return src[:http.end] + "\n" + line + src[http.end:], nil
}

// defaultConfDir is where nginx keeps its configuration on this system when
// it is not installed yet.
func defaultConfDir() string {
	if hostOS == "darwin" {
		if runtime.GOARCH == "arm64" {
			return "/opt/homebrew/etc/nginx"
		}
		return "/usr/local/etc/nginx"
	}
	return "/etc/nginx"
}

// NginxConfDir returns the directory of the main nginx configuration file.
func NginxConfDir() string {
	if conf := ReadNginxCompileInfo().NginxConfPath; conf != "" {
		return filepath.Dir(conf)
	}
	return defaultConfDir()
}

// NginxPidFile returns the pid file of the nginx master: the pid directive of
// the main configuration when it sets one, otherwise the compiled-in path.
func NginxPidFile() string {
	info := ReadNginxCompileInfo()
	if info.NginxConfPath == "" {
		return info.NginxPidPath
	}
	if src, err := os.ReadFile(info.NginxConfPath); err == nil {
		if directives, err := parseConf(string(src)); err == nil {
			if pid := pidDirective(directives); pid != "" {
				return underPrefix(info.Prefix, pid, "")
			}
		}
	}
	return info.NginxPidPath
}

// SitesLocation is the directory Uranus keeps site configurations in.
type SitesLocation struct {
	Dir string
	// Included reports whether the http block of the main configuration
	// includes Dir, i.e. whether nginx loads the sites saved there.
	Included bool
	// Checked is false when the main configuration could not be read or
	// parsed (or nginx is not installed); Included says nothing then.
	Checked bool
}

// Sites returns where site configurations go: the directory set in
// config.toml, otherwise the first directory nginx includes, otherwise conf.d
// next to nginx.conf.
func Sites() SitesLocation {
	configured := GetAppConfig().VhostPath
	confPath := ReadNginxCompileInfo().NginxConfPath
	confDir := NginxConfDir()
	if confPath != "" {
		if src, err := os.ReadFile(confPath); err == nil {
			if dirs, err := includedSiteDirs(string(src), confDir); err == nil {
				return chooseSitesDir(configured, dirs, confDir)
			}
		}
	}
	if configured == "" {
		configured = filepath.Join(confDir, "conf.d")
	}
	return SitesLocation{Dir: configured}
}

// SitesDir returns the directory for site configuration files.
func SitesDir() string {
	return Sites().Dir
}

func chooseSitesDir(configured string, included []string, confDir string) SitesLocation {
	if configured != "" {
		configured = filepath.Clean(configured)
		// 用户指定的目录始终使用，没被引用时页面会提示；旧版本写入的默认值只在 nginx 确实引用它时使用
		if configured != legacySitesDir || slices.Contains(included, configured) {
			return SitesLocation{Dir: configured, Included: slices.Contains(included, configured), Checked: true}
		}
	}
	if len(included) > 0 {
		return SitesLocation{Dir: included[0], Included: true, Checked: true}
	}
	return SitesLocation{Dir: filepath.Join(confDir, "conf.d"), Checked: true}
}

// SSLDir returns the directory for certificates: the configured one, or ssl
// next to nginx.conf.
func SSLDir() string {
	return chooseSSLDir(GetAppConfig().SSLPath, NginxConfDir())
}

func chooseSSLDir(configured, confDir string) string {
	if configured = ignoreLegacyDefault(configured, legacySSLDir, confDir); configured == "" {
		return filepath.Join(confDir, "ssl")
	}
	return configured
}

// ignoreLegacyDefault drops a directory that is only the default earlier
// versions wrote to config.toml, when nginx does not live in /etc/nginx
// (macOS, OpenResty, built from source) and the directory was never created.
// A directory in use is kept: site configurations name certificates by their
// absolute path.
func ignoreLegacyDefault(configured, legacy, confDir string) string {
	if configured == "" || filepath.Clean(configured) != legacy || confDir == "/etc/nginx" {
		return configured
	}
	if _, err := os.Stat(configured); os.IsNotExist(err) {
		return ""
	}
	return configured
}
