package main

import (
	"bytes"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"time"

	"uranus/internal/config"
)

// svgIcon renders nothing for a missing icon, so a misspelt name would only
// show up as a gap in the page.
func TestTemplatesUseExistingIcons(t *testing.T) {
	iconRef := regexp.MustCompile(`svgIcon "([^"]+)"`)
	found := 0
	err := fs.WalkDir(templates, "web", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		content, err := templates.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range iconRef.FindAllStringSubmatch(string(content), -1) {
			found++
			if _, err := staticFS.ReadFile("web/public/icons/" + m[1] + ".svg"); err != nil {
				t.Errorf("%s uses the missing icon %q", path, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("no svgIcon calls found")
	}
}

// usage has the fields the dashboard reads from controllers.usage.
type usage struct {
	Used, Total string
	Percent     float64
	Level       string
}

func dashboardData() map[string]any {
	return map[string]any{
		"activePage":       "dashboard",
		"nginxInstalled":   true,
		"nginxStatus":      "1234",
		"nginxCompileInfo": &config.NginxCompileInfo{Version: "nginx/1.27.0", NginxConfPath: "/etc/nginx/nginx.conf"},
		"buildTime":        "2026-11-18 23:47:18",
		"buildVersion":     "v1.0.0",
		"commitID":         "17e4e9696f35f40f57890d540301fc6f9ff76f76",
		"commitShort":      "17e4e969",
		"cpuCount":         4,
		"hostname":         "web-1",
		"osName":           "ubuntu 24.04",
		"uptime":           "3 天 4 小时",
		"cpuModel":         "AMD EPYC",
		"load":             []string{"0.10", "0.20", "0.30"},
		"mem":              &usage{"1.0 GiB", "4.0 GiB", 25, "ok"},
		"disk":             &usage{"30 GiB", "40 GiB", 75, "warn"},
		"sitesCount":       3,
		"certsCount":       2,
		"certsExpiring":    1,
	}
}

// Template errors only happen when a page is rendered, and then the page is
// blank: render the dashboard in each state it can be in.
func TestDashboardTemplate(t *testing.T) {
	tmpl, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name          string
		change        func(map[string]any)
		want, notWant []string
	}{
		{"running", func(map[string]any) {},
			[]string{`value="reload"`, `value="restart"`, `value="stop"`, "主进程 PID 1234"},
			[]string{`value="start"`, "未检测到 Nginx"}},
		{"stopped", func(d map[string]any) { d["nginxStatus"] = "KO" },
			[]string{`value="start"`, "已停止"},
			[]string{`value="reload"`, `value="restart"`, `value="stop"`}},
		{"nginx not installed", func(d map[string]any) { d["nginxInstalled"] = false; d["nginxStatus"] = "KO" },
			[]string{"未检测到 Nginx"},
			[]string{`value="start"`, `value="stop"`}},
		{"restarted", func(d map[string]any) { d["nginxAction"] = "restart"; d["nginxActionMessage"] = "OK" },
			[]string{"Nginx 已重启"}, nil},
		{"reload refused", func(d map[string]any) {
			d["nginxAction"] = "reload"
			d["nginxActionMessage"] = "配置检查未通过：\nunknown directive"
		}, []string{"操作没有完成", "unknown directive"}, []string{"Nginx 已重新加载配置"}},
		// Built from a source archive without .git: the commit id is empty.
		{"no commit id", func(d map[string]any) { d["commitID"] = ""; d["commitShort"] = "" }, nil, nil},
		// The system details are left out when they cannot be read.
		{"no system details", func(d map[string]any) {
			for _, key := range []string{"hostname", "osName", "uptime", "cpuModel", "load", "mem", "disk"} {
				delete(d, key)
			}
		}, nil, []string{"<no value>"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := dashboardData()
			c.change(data)
			var out bytes.Buffer
			if err := tmpl.ExecuteTemplate(&out, "index.html", data); err != nil {
				t.Fatal(err)
			}
			page := out.String()
			for _, s := range c.want {
				if !strings.Contains(page, s) {
					t.Errorf("page lacks %q", s)
				}
			}
			for _, s := range c.notWant {
				if strings.Contains(page, s) {
					t.Errorf("page contains %q", s)
				}
			}
		})
	}
}

func render(t *testing.T, page string, data map[string]any) string {
	t.Helper()
	tmpl, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, page, data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// The sites page warns when nginx does not load its directory, and offers to
// add the include.
func TestSitesTemplate(t *testing.T) {
	data := map[string]any{"activePage": "sites", "files": []any{}, "sitesDir": "/usr/local/nginx/conf/conf.d",
		"nginxConf": "/usr/local/nginx/conf/nginx.conf", "notIncluded": true}
	if page := render(t, "sites.html", data); !strings.Contains(page, `action="/admin/sites/include"`) ||
		!strings.Contains(page, "/usr/local/nginx/conf/conf.d") {
		t.Fatal("no warning about the sites directory")
	}
	data["notIncluded"] = false
	if page := render(t, "sites.html", data); strings.Contains(page, `action="/admin/sites/include"`) {
		t.Fatal("warning shown for an included directory")
	}
	data["included"] = "OK"
	if page := render(t, "sites.html", data); !strings.Contains(page, "这里的网站现在会生效") {
		t.Fatal("include result not shown")
	}
}

func TestNginxConfigTemplate(t *testing.T) {
	info := &config.NginxCompileInfo{Version: "openresty/1.25.3.1", NginxExec: "/usr/local/openresty/nginx/sbin/nginx",
		NginxConfPath: "/usr/local/openresty/nginx/conf/nginx.conf"}
	page := render(t, "config.html", map[string]any{
		"activePage": "config", "nginxCompileInfo": info, "installed": true,
		"pidFile": "/usr/local/openresty/nginx/logs/nginx.pid",
		"sites":   config.SitesLocation{Dir: "/usr/local/openresty/nginx/conf/conf.d", Checked: true},
		"sslDir":  "/usr/local/openresty/nginx/conf/ssl", "manager": "systemd（openresty.service）",
	})
	for _, want := range []string{"/usr/local/openresty/nginx/sbin/nginx", "/usr/local/openresty/nginx/logs/nginx.pid",
		"未被主配置引用", "/usr/local/openresty/nginx/conf/ssl", "systemd（openresty.service）"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if page := render(t, "config.html", map[string]any{"activePage": "config",
		"nginxCompileInfo": &config.NginxCompileInfo{}}); !strings.Contains(page, "没有找到 Nginx") {
		t.Error("missing nginx not shown")
	}
}

// Deleting a certificate a site uses is refused with the list of files; a
// deleted site whose certificate another site uses keeps the certificate.
func TestCertificateDeletionResults(t *testing.T) {
	page := render(t, "ssl.html", map[string]any{"activePage": "ssl", "deleteError": "证书 blog.example.com 还在被下面的配置使用，先修改或删除这些网站：\n/etc/nginx/conf.d/www.example.com.conf"})
	if !strings.Contains(page, "没有删除证书") || !strings.Contains(page, "/etc/nginx/conf.d/www.example.com.conf") {
		t.Fatal("refusal not shown")
	}
	if page := render(t, "ssl.html", map[string]any{"activePage": "ssl", "deleted": "blog.example.com"}); !strings.Contains(page, "已删除证书 blog.example.com") {
		t.Fatal("deletion not shown")
	}
	page = render(t, "sites.html", map[string]any{"activePage": "sites", "files": []any{},
		"deleted": "blog.example.com.conf", "message": "OK", "certKept": "/etc/nginx/conf.d/www.example.com.conf"})
	if !strings.Contains(page, "已删除 blog.example.com.conf") || !strings.Contains(page, "已保留并继续续签") ||
		!strings.Contains(page, "/etc/nginx/conf.d/www.example.com.conf") {
		t.Fatal("kept certificate not shown")
	}
}

// The stylesheet is cached for a day: its URL carries the build's commit, so
// pages of a new release don't keep the previous release's styles.
func TestStylesheetURLChangesWithRelease(t *testing.T) {
	old := config.CommitID
	config.CommitID = "abc1234"
	t.Cleanup(func() { config.CommitID = old })
	tmpl, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{"login.html", "header.html"} {
		var out strings.Builder
		if err := tmpl.ExecuteTemplate(&out, page, map[string]any{}); err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		if !strings.Contains(out.String(), `href="/public/css/tailwind.min.css?v=abc1234"`) {
			t.Errorf("%s: stylesheet URL without the release:\n%s", page, out.String())
		}
	}
}

// Enabled sites can be disabled (a link only, not deleted: deleting it would
// lose nothing but the link); disabled sites can be enabled or deleted.
func TestSitesTemplateDisabledSites(t *testing.T) {
	type row struct {
		Name, Path, Dir, Link, Size string
		ModTime                     time.Time
		DisabledID                  uint
	}
	data := map[string]any{"activePage": "sites", "sitesDir": "/etc/nginx/conf.d",
		"files": []row{
			{Name: "blog.conf", Path: "/etc/nginx/conf.d/blog.conf", Dir: "/etc/nginx/conf.d"},
			{Name: "default", Path: "/etc/nginx/sites-enabled/default", Dir: "/etc/nginx/sites-enabled", Link: "/etc/nginx/sites-available/default"},
		},
		"disabled": []row{{Name: "shop.conf", Path: "/etc/nginx/conf.d/shop.conf", Dir: "/etc/nginx/conf.d", DisabledID: 7}},
	}
	page := render(t, "sites.html", data)
	if n := strings.Count(page, `action="/admin/sites/disable"`); n != 2 {
		t.Fatalf("%d disable buttons, want 2", n)
	}
	if n := strings.Count(page, `action="/admin/sites/delete"`); n != 1 {
		t.Fatalf("%d delete buttons for enabled sites, want 1 (not for the link)", n)
	}
	for _, want := range []string{"已停用", `action="/admin/sites/enable"`, `action="/admin/sites/delete-disabled"`, `name="id" value="7"`, "另有 1 个已停用"} {
		if !strings.Contains(page, want) {
			t.Fatalf("missing %q", want)
		}
	}

	data["disabledName"], data["message"] = "blog.conf", "OK"
	if page := render(t, "sites.html", data); !strings.Contains(page, "已停用 blog.conf") {
		t.Fatal("disable result not shown")
	}
	data["disabledName"], data["enabled"], data["message"] = "", "shop.conf", "加上这个网站后配置检查不通过"
	if page := render(t, "sites.html", data); !strings.Contains(page, "启用 shop.conf 没有完成") || !strings.Contains(page, "配置检查不通过") {
		t.Fatal("failed enable not shown")
	}
}

func TestConfigFilePages(t *testing.T) {
	type row struct {
		Name, Rel, Link, SizeText string
		IsDir                     bool
		ModTime                   time.Time
	}
	page := render(t, "files.html", map[string]any{
		"activePage": "config", "confDir": "/etc/nginx", "rel": "sites-enabled",
		"crumbs": []map[string]string{{"Name": "sites-enabled", "Rel": "sites-enabled"}},
		"files": []row{
			{Name: "default", Rel: "sites-enabled/default", Link: "/etc/nginx/sites-available/default", SizeText: "2.4 kB"},
			{Name: "extra", Rel: "sites-enabled/extra", IsDir: true},
		},
	})
	for _, want := range []string{`href="/admin/nginx/file?path=sites-enabled%2fdefault"`, `href="/admin/nginx/files?dir=sites-enabled%2fextra"`,
		"→ /<wbr>etc/<wbr>nginx/<wbr>sites-<wbr>available/<wbr>default", `id="checkConf"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("file list misses %q", want)
		}
	}
	if page := render(t, "files.html", map[string]any{"confDir": "/etc/nginx", "error": "没有这个文件或目录"}); !strings.Contains(page, "没有这个文件或目录") || strings.Contains(page, "这个目录是空的") {
		t.Fatal("error not shown alone")
	}

	page = render(t, "fileEdit.html", map[string]any{
		"activePage": "config", "rel": "conf.d/blog.conf", "parent": "conf.d", "name": "blog.conf",
		"abs": "/etc/nginx/conf.d/blog.conf", "content": "server {}\n",
	})
	for _, want := range []string{`data-path="conf.d/blog.conf"`, `href="/admin/nginx/files?dir=conf.d"`, `id="openHistory"`,
		`data-path="/etc/nginx/conf.d/blog.conf"`, `id="checkConf"`, `id="history-modal"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("editor misses %q", want)
		}
	}
}

func TestLogsTemplate(t *testing.T) {
	page := render(t, "logs.html", map[string]any{"activePage": "logs",
		"files": []string{"/var/log/nginx/error.log", "/var/log/nginx/access.log"}, "selected": "/var/log/nginx/access.log"})
	for _, want := range []string{`<option value="/var/log/nginx/access.log" selected>`, `id="logView"`, `href="/admin/nginx/logs" class="nav-link active"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("log page misses %q", want)
		}
	}
	if page := render(t, "logs.html", map[string]any{"activePage": "logs"}); !strings.Contains(page, "没有找到日志文件") || strings.Contains(page, `id="logView"`) {
		t.Fatal("empty state not shown")
	}
}
