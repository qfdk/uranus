package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Default main configuration files of the nginx packages, shortened but with
// their structure, comments and quoting intact.

const debianConf = `user www-data;
worker_processes auto;
pid /run/nginx.pid;
error_log /var/log/nginx/error.log;
include /etc/nginx/modules-enabled/*.conf;

events {
	worker_connections 768;
	# multi_accept on;
}

http {
	sendfile on;
	tcp_nopush on;
	types_hash_max_size 2048;
	# server_tokens off;

	include /etc/nginx/mime.types;
	default_type application/octet-stream;

	ssl_protocols TLSv1 TLSv1.1 TLSv1.2 TLSv1.3; # Dropping SSLv3, ref: POODLE
	ssl_prefer_server_ciphers on;

	access_log /var/log/nginx/access.log;
	gzip on;

	##
	# Virtual Host Configs
	##

	include /etc/nginx/conf.d/*.conf;
	include /etc/nginx/sites-enabled/*;
}


#mail {
#	# auth_http localhost/auth.php;
#	# pop3_capabilities "TOP" "USER";
#
#	server {
#		listen     localhost:110;
#		protocol   pop3;
#	}
#}
`

const nginxOrgConf = `
user  nginx;
worker_processes  auto;

error_log  /var/log/nginx/error.log notice;
pid        /run/nginx.pid;


events {
    worker_connections  1024;
}


http {
    include       /etc/nginx/mime.types;
    default_type  application/octet-stream;

    log_format  main  '$remote_addr - $remote_user [$time_local] "$request" '
                      '$status $body_bytes_sent "$http_referer" '
                      '"$http_user_agent" "$http_x_forwarded_for"';

    access_log  /var/log/nginx/access.log  main;

    sendfile        on;
    #tcp_nopush     on;

    keepalive_timeout  65;

    #gzip  on;

    include /etc/nginx/conf.d/*.conf;
}
`

const rhelConf = `# For more information on configuration, see:
#   * Official English Documentation: http://nginx.org/en/docs/

user nginx;
worker_processes auto;
error_log /var/log/nginx/error.log;
pid /run/nginx.pid;

# Load dynamic modules. See /usr/share/doc/nginx/README.dynamic.
include /usr/share/nginx/modules/*.conf;

events {
    worker_connections 1024;
}

http {
    log_format  main  '$remote_addr - $remote_user [$time_local] "$request" '
                      '$status $body_bytes_sent "$http_referer" '
                      '"$http_user_agent" "$http_x_forwarded_for"';

    access_log  /var/log/nginx/access.log  main;

    sendfile            on;
    keepalive_timeout   65;
    types_hash_max_size 4096;

    include             /etc/nginx/mime.types;
    default_type        application/octet-stream;

    # Load modular configuration files from the /etc/nginx/conf.d directory.
    include /etc/nginx/conf.d/*.conf;

    server {
        listen       80;
        listen       [::]:80;
        server_name  _;
        root         /usr/share/nginx/html;

        # Load configuration files for the default server block.
        include /etc/nginx/default.d/*.conf;

        error_page 404 /404.html;
        location = /404.html {
        }
    }
}
`

const alpineConf = `# /etc/nginx/nginx.conf

user nginx;

# Set number of worker processes automatically based on number of CPU cores.
worker_processes auto;

# Enables the use of JIT for regular expressions to speed-up their processing.
pcre_jit on;

# Configures default error logger.
error_log /var/log/nginx/error.log warn;

# Includes files with directives to load dynamic modules.
include /etc/nginx/modules/*.conf;

# Include files with config snippets into the root context.
include /etc/nginx/conf.d/*.conf;

events {
	# The maximum number of simultaneous connections that can be opened by
	# a worker process.
	worker_connections 1024;
}

http {
	# Includes mapping of file name extensions to MIME types of responses
	# and defines the default type.
	include /etc/nginx/mime.types;
	default_type application/octet-stream;

	# Don't tell nginx version to the clients. Default is 'on'.
	server_tokens off;

	# Path of the file with Diffie-Hellman parameters for EDH ciphers.
	# TIP: Generate with: ` + "`openssl dhparam -out /etc/ssl/nginx/dh2048.pem 2048`" + `
	#ssl_dhparam /etc/ssl/nginx/dh2048.pem;

	# Helper variable for proxying websockets.
	map $http_upgrade $connection_upgrade {
		default upgrade;
		'' close;
	}

	# Specifies the main log format.
	log_format main '$remote_addr - $remote_user [$time_local] "$request" '
			'$status $body_bytes_sent "$http_referer" '
			'"$http_user_agent" "$http_x_forwarded_for"';

	# Includes virtual hosts configs.
	include /etc/nginx/http.d/*.conf;
}

# TIP: Uncomment if you use stream module.
#include /etc/nginx/stream.conf;
`

// stockConf is conf/nginx.conf of the nginx sources: what a source build,
// OpenResty and Arch Linux install. Homebrew appends "include servers/*;".
const stockConf = `
#user  nobody;
worker_processes  1;

#error_log  logs/error.log;
#pid        logs/nginx.pid;


events {
    worker_connections  1024;
}


http {
    include       mime.types;
    default_type  application/octet-stream;

    #log_format  main  '$remote_addr - $remote_user [$time_local] "$request" '
    #                  '$status $body_bytes_sent "$http_referer" '
    #                  '"$http_user_agent" "$http_x_forwarded_for"';

    sendfile        on;
    keepalive_timeout  65;

    server {
        listen       80;
        server_name  localhost;

        location / {
            root   html;
            index  index.html index.htm;
        }

        # redirect server error pages to the static page /50x.html
        #
        error_page   500 502 503 504  /50x.html;
        location = /50x.html {
            root   html;
        }
    }


    # HTTPS server
    #
    #server {
    #    listen       443 ssl;
    #    server_name  localhost;
    #}

}
`

var homebrewConf = func() string {
	end := strings.LastIndex(stockConf, "\n}\n") // the end of the http block
	return stockConf[:end] + "\n    include servers/*;" + stockConf[end:]
}()

func TestIncludedSiteDirs(t *testing.T) {
	cases := []struct {
		name, src, confDir string
		want               []string
		pid                string
	}{
		{"Debian and Ubuntu packages", debianConf, "/etc/nginx", []string{"/etc/nginx/conf.d", "/etc/nginx/sites-enabled"}, "/run/nginx.pid"},
		{"nginx.org packages and Docker image", nginxOrgConf, "/etc/nginx", []string{"/etc/nginx/conf.d"}, "/run/nginx.pid"},
		// default.d is included inside the default server block, not the http block
		{"CentOS, Rocky, Alma", rhelConf, "/etc/nginx", []string{"/etc/nginx/conf.d"}, "/run/nginx.pid"},
		// conf.d is included at the top level: a server block there is an error
		{"Alpine", alpineConf, "/etc/nginx", []string{"/etc/nginx/http.d"}, ""},
		{"Homebrew", homebrewConf, "/opt/homebrew/etc/nginx", []string{"/opt/homebrew/etc/nginx/servers"}, ""},
		{"source build, OpenResty, Arch", stockConf, "/usr/local/nginx/conf", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dirs, err := includedSiteDirs(c.src, c.confDir)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(dirs, "|") != strings.Join(c.want, "|") {
				t.Fatalf("included site dirs = %q, want %q", dirs, c.want)
			}
			directives, _ := parseConf(c.src)
			if pid := pidDirective(directives); pid != c.pid {
				t.Fatalf("pid = %q, want %q", pid, c.pid)
			}
		})
	}
}

// Comments, quotes and ${var} must not be mistaken for block or directive ends.
func TestParseConfTricky(t *testing.T) {
	src := `http {
    # a comment with { braces } and ; semicolons
    log_format x '{"a": "$b"}' "c;d#e" 'it\'s';
    set $x "${host}abc";
    return 200 ${host}/#not-a-comment;
    map $a $b { default 1; } # } after a block
    include "/srv/my sites/*.conf";
    include conf.d/*.conf; # trailing comment
    include snippets/ssl.conf;
    include /etc/nginx/*/sites/*.conf;
}
`
	dirs, err := includedSiteDirs(src, "/etc/nginx")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/srv/my sites", "/etc/nginx/conf.d"}
	if strings.Join(dirs, "|") != strings.Join(want, "|") {
		t.Fatalf("included site dirs = %q, want %q", dirs, want)
	}

	directives, _ := parseConf(src)
	http := httpBlock(directives)
	if got := http.block[0].args; strings.Join(got, "|") != `x|{"a": "$b"}|c;d#e|it's` {
		t.Fatalf("log_format args = %q", got)
	}
	if got := http.block[2].args; strings.Join(got, "|") != "200|${host}/#not-a-comment" {
		t.Fatalf("return args = %q", got)
	}
}

// http { include vhosts.conf; } with the site directories included from there.
func TestIncludedSiteDirsFollowsIncludedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vhosts.conf"), []byte("include /srv/sites/*.conf;\ninclude more.conf;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "more.conf"), []byte("include extra/*;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs, err := includedSiteDirs("http {\n    include mime.types;\n    include vhosts.conf;\n}\n", dir)
	want := []string{"/srv/sites", filepath.Join(dir, "extra")}
	if err != nil || strings.Join(dirs, "|") != strings.Join(want, "|") {
		t.Fatalf("included site dirs = %q %v, want %q", dirs, err, want)
	}
}

func TestParseConfErrors(t *testing.T) {
	for _, src := range []string{"http {", "http { }}", "worker_processes 1", "http { listen 80 }", `log_format "x;`, "; events {}"} {
		if _, err := parseConf(src); err == nil {
			t.Errorf("no error for %q", src)
		}
	}
}

func TestAddSitesInclude(t *testing.T) {
	updated, err := AddSitesInclude(stockConf, "/usr/local/nginx/conf/conf.d")
	if err != nil {
		t.Fatal(err)
	}
	dirs, err := includedSiteDirs(updated, "/usr/local/nginx/conf")
	if err != nil || len(dirs) != 1 || dirs[0] != "/usr/local/nginx/conf/conf.d" {
		t.Fatalf("include not added to the http block: %q %v\n%s", dirs, err, updated)
	}
	// One line added right before the closing brace of http, indented like the
	// directives above it; nothing else changes.
	wantTail := "    #}\n\n    include /usr/local/nginx/conf/conf.d/*.conf;\n}\n"
	if !strings.HasSuffix(updated, wantTail) || len(updated) != len(stockConf)+len("    include /usr/local/nginx/conf/conf.d/*.conf;\n") {
		t.Fatalf("unexpected result:\n%s", updated)
	}

	tabs, _ := AddSitesInclude(debianConf, "/srv/sites")
	if !strings.Contains(tabs, "\tinclude /etc/nginx/sites-enabled/*;\n\tinclude /srv/sites/*.conf;\n}") {
		t.Fatalf("tab indentation not kept:\n%s", tabs)
	}

	oneLine, err := AddSitesInclude("events {} http { server { listen 80; } }", "/srv/my sites")
	if err != nil {
		t.Fatal(err)
	}
	if dirs, _ := includedSiteDirs(oneLine, "/etc/nginx"); len(dirs) != 1 || dirs[0] != "/srv/my sites" {
		t.Fatalf("one-line http block: %q\n%s", dirs, oneLine)
	}

	if _, err := AddSitesInclude("events {}\nstream { }\n", "/srv/sites"); err == nil {
		t.Fatal("no error without an http block")
	}
}

func TestChooseSitesDir(t *testing.T) {
	debian := []string{"/etc/nginx/conf.d", "/etc/nginx/sites-enabled"}
	cases := []struct {
		name, configured string
		included         []string
		want             SitesLocation
	}{
		{"configured and included", "/etc/nginx/sites-enabled", debian,
			SitesLocation{Dir: "/etc/nginx/sites-enabled", Included: true, Checked: true}},
		// a directory the user chose is kept; the sites page warns
		{"configured, not included", "/srv/sites", debian,
			SitesLocation{Dir: "/srv/sites", Checked: true}},
		{"old default on Debian", legacySitesDir, debian,
			SitesLocation{Dir: "/etc/nginx/conf.d", Included: true, Checked: true}},
		// earlier versions wrote /etc/nginx/conf.d everywhere; on Alpine nginx loads http.d
		{"old default on Alpine", legacySitesDir, []string{"/etc/nginx/http.d"},
			SitesLocation{Dir: "/etc/nginx/http.d", Included: true, Checked: true}},
		{"nothing configured", "", []string{"/opt/homebrew/etc/nginx/servers"},
			SitesLocation{Dir: "/opt/homebrew/etc/nginx/servers", Included: true, Checked: true}},
		{"nothing configured or included", "", nil,
			SitesLocation{Dir: "/etc/nginx/conf.d", Checked: true}},
	}
	for _, c := range cases {
		if got := chooseSitesDir(c.configured, c.included, "/etc/nginx"); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// Earlier versions wrote /etc/nginx/conf.d and /etc/nginx/ssl into every
// config.toml, also where nginx does not live in /etc/nginx.
func TestLegacyDefaultDirs(t *testing.T) {
	openresty := "/usr/local/openresty/nginx/conf"
	if got := chooseSitesDir(legacySitesDir, nil, openresty); got.Dir != openresty+"/conf.d" {
		t.Errorf("OpenResty sites dir = %q", got.Dir)
	}
	if _, err := os.Stat(legacySSLDir); err == nil {
		t.Skipf("%s exists on this machine", legacySSLDir)
	}
	if got := chooseSSLDir(legacySSLDir, openresty); got != openresty+"/ssl" {
		t.Errorf("OpenResty ssl dir = %q", got)
	}
	if got := chooseSSLDir(legacySSLDir, "/etc/nginx"); got != legacySSLDir {
		t.Errorf("Debian ssl dir = %q", got)
	}
	if got := chooseSSLDir("", "/opt/homebrew/etc/nginx"); got != "/opt/homebrew/etc/nginx/ssl" {
		t.Errorf("Homebrew ssl dir = %q", got)
	}

	// Certificates already stored there keep their directory: site
	// configurations name them by absolute path.
	inUse := t.TempDir()
	if got := chooseSSLDir(inUse, openresty); got != inUse {
		t.Errorf("configured ssl dir replaced: %q", got)
	}
}

// useFakeNginx makes detection find a fake nginx whose -V reports prefix and
// the main configuration conf.
func useFakeNginx(t *testing.T, prefix, conf string) {
	t.Helper()
	resetNginxDetection(t)
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "nginx"), "#!/bin/sh\n"+
		"echo 'nginx version: nginx/1.27.0' >&2\n"+
		"echo 'configure arguments: --prefix="+prefix+" --conf-path="+conf+" --pid-path="+prefix+"/logs/nginx.pid' >&2\n")
	t.Setenv("PATH", bin)
	nginxSearchPaths = nil
}

func TestNginxPidFileFollowsThePidDirective(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	useFakeNginx(t, dir, conf)

	for content, want := range map[string]string{
		"events {}\n":                              filepath.Join(dir, "logs/nginx.pid"),
		"pid /run/custom.pid;\nevents {}\n":        "/run/custom.pid",
		"pid run/relative.pid;\nevents {}\n":       filepath.Join(dir, "run/relative.pid"),
		"#pid /run/commented.pid;\nevents {}\n":    filepath.Join(dir, "logs/nginx.pid"),
		"http { pid /run/not-main.pid; }\n":        filepath.Join(dir, "logs/nginx.pid"),
		"pid /run/broken.pid\nevents { unclosed\n": filepath.Join(dir, "logs/nginx.pid"),
	} {
		if err := os.WriteFile(conf, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := NginxPidFile(); got != want {
			t.Errorf("%q: pid file %q, want %q", content, got, want)
		}
	}
}

func TestSitesReadsTheMainConfig(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	useFakeNginx(t, dir, conf)

	if got := Sites(); got != (SitesLocation{Dir: filepath.Join(dir, "conf.d")}) {
		t.Fatalf("missing nginx.conf: %+v", got)
	}
	if err := os.WriteFile(conf, []byte(homebrewConf), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Sites(); got != (SitesLocation{Dir: filepath.Join(dir, "servers"), Included: true, Checked: true}) {
		t.Fatalf("Homebrew layout: %+v", got)
	}
	if got := SSLDir(); got != filepath.Join(dir, "ssl") {
		t.Fatalf("ssl dir = %q", got)
	}
}

// A certificate must not be deleted while nginx loads a file that uses it: the
// next reload would fail. The dump is the output of `nginx -T`.
func TestConfigFilesUsing(t *testing.T) {
	dump := "# configuration file /etc/nginx/nginx.conf:\n" +
		"http {\n    include /etc/nginx/conf.d/*.conf;\n    include sites-enabled/*;\n}\n\n" +
		"# configuration file /etc/nginx/conf.d/blog.conf:\n" +
		"server {\n    listen 443 ssl;\n    ssl_certificate /etc/nginx/ssl/blog/fullchain.cer;\n" +
		"    ssl_certificate_key /etc/nginx/ssl/blog/private.key;\n}\n\n" +
		"# configuration file /etc/nginx/conf.d/blog2.conf:\n" +
		"server {\n    ssl_certificate /etc/nginx/ssl/blog2/fullchain.cer;\n}\n\n" +
		"# configuration file /etc/nginx/conf.d/old.conf:\n" +
		"server {\n    listen 80;\n    # ssl_certificate /etc/nginx/ssl/blog/fullchain.cer;\n}\n\n" +
		"# configuration file /etc/nginx/sites-enabled/default:\n" +
		"server {\n    ssl_certificate_key \"ssl/blog/private.key\";\n}\n\n"

	got := ConfigFilesUsing(dump, "/etc/nginx/ssl/blog/", "/etc/nginx")
	want := []string{"/etc/nginx/conf.d/blog.conf", "/etc/nginx/sites-enabled/default"}
	if !slices.Equal(got, want) {
		t.Fatalf("files using the certificate: %q, want %q", got, want)
	}
	if got := ConfigFilesUsing(dump, "/etc/nginx/ssl/shop", "/etc/nginx"); len(got) != 0 {
		t.Fatalf("unused certificate reported as used by %q", got)
	}
	// A file that does not parse is searched as text, so a broken site still counts.
	broken := "# configuration file /etc/nginx/conf.d/broken.conf:\n" +
		"server {\n    ssl_certificate \"/etc/nginx/ssl/blog/fullchain.cer;\n"
	if got := ConfigFilesUsing(broken, "/etc/nginx/ssl/blog", "/etc/nginx"); !slices.Equal(got, []string{"/etc/nginx/conf.d/broken.conf"}) {
		t.Fatalf("broken file: %q", got)
	}
}

// Debian and Ubuntu load sites from conf.d/*.conf and from sites-enabled/*,
// where files often have no .conf and are links into sites-available. A site
// in a directory Uranus did not list could not be seen, edited or deleted,
// and deleting its certificate left nginx unable to reload.
func TestSiteFiles(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	useFakeNginx(t, dir, conf)
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("nginx.conf", "events {}\nhttp {\n    include "+dir+"/conf.d/*.conf;\n    include sites-enabled/*;\n}\n")
	blog := write("conf.d/blog.example.com.conf", "server {}")
	write("conf.d/notes.txt", "not loaded by nginx")
	write("conf.d/.hidden.conf", "not matched by nginx's glob")
	write("conf.d/old/archive.conf", "in a subdirectory")
	defaultSite := write("sites-available/default", "server { listen 80 default_server; }")
	shop := write("sites-enabled/shop.example.com.conf", "server {}")
	enabledDefault := filepath.Join(dir, "sites-enabled", "default")
	if err := os.Symlink("../sites-available/default", enabledDefault); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "sites-enabled", "broken")); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, site := range SiteFiles() {
		got = append(got, site.Path+" -> "+site.Link)
	}
	want := []string{blog + " -> ", enabledDefault + " -> " + defaultSite, shop + " -> "}
	if !slices.Equal(got, want) {
		t.Fatalf("SiteFiles:\n%q\nwant\n%q", got, want)
	}

	if site, ok := FindSiteFile(enabledDefault); !ok || site.Name != "default" || site.ConfigName() != "default" {
		t.Fatalf("FindSiteFile(%s) = %+v, %v", enabledDefault, site, ok)
	}
	if site, ok := FindSiteFile(blog); !ok || site.ConfigName() != "blog.example.com" {
		t.Fatalf("FindSiteFile(%s) = %+v, %v", blog, site, ok)
	}
	for _, path := range []string{conf, filepath.Join(dir, "conf.d", "notes.txt"),
		filepath.Join(dir, "conf.d", "..", "nginx.conf"), defaultSite, "", "conf.d/blog.example.com.conf"} {
		if site, ok := FindSiteFile(path); ok {
			t.Errorf("%q is not a site, got %+v", path, site)
		}
	}
}

// Built from source, nginx includes no directory: the sites Uranus saved are
// still listed, with the warning that nginx does not load them.
func TestSiteFilesOfADirectoryNginxDoesNotInclude(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	useFakeNginx(t, dir, conf)
	if err := os.WriteFile(conf, []byte("events {}\nhttp {\n    server { listen 80; }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(dir, "conf.d", "blog.example.com.conf")
	if err := os.MkdirAll(filepath.Dir(site), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(site, []byte("server {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if files := SiteFiles(); len(files) != 1 || files[0].Path != site {
		t.Fatalf("SiteFiles() = %+v", files)
	}
}
