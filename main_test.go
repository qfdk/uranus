package main

import (
	"bytes"
	"io/fs"
	"regexp"
	"strings"
	"testing"

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
