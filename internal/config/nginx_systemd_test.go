package config

import (
	"path/filepath"
	"testing"
)

func TestParseSystemdShow(t *testing.T) {
	cases := []struct {
		name, out string
		want      SystemdUnit
	}{
		{"Debian and Ubuntu",
			"ExecStart={ path=/usr/sbin/nginx ; argv[]=/usr/sbin/nginx -g daemon on; master_process on; ; ignore_errors=no ; start_time=[Tue 2026-11-17 10:00:00 UTC] ; stop_time=[n/a] ; pid=812 ; code=(null) ; status=0/0 }\nLoadState=loaded\nActiveState=active\n",
			SystemdUnit{Loaded: true, Active: true, ExecPath: "/usr/sbin/nginx"}},
		// systemctl show does not expand the variable
		{"nginx.org",
			"ExecStart={ path=/usr/sbin/nginx ; argv[]=/usr/sbin/nginx -c ${CONFFILE} ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }\nLoadState=loaded\nActiveState=inactive\n",
			SystemdUnit{Loaded: true, ExecPath: "/usr/sbin/nginx"}},
		{"custom main config",
			"LoadState=loaded\nActiveState=reloading\nExecStart={ path=/opt/nginx/sbin/nginx ; argv[]=/opt/nginx/sbin/nginx -c /srv/nginx/main.conf ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }\n",
			SystemdUnit{Loaded: true, Active: true, ExecPath: "/opt/nginx/sbin/nginx", ConfArg: "/srv/nginx/main.conf"}},
		{"not installed", "LoadState=not-found\nActiveState=inactive\nExecStart=\n", SystemdUnit{}},
	}
	for _, c := range cases {
		c.want.Name = "nginx.service"
		if got := parseSystemdShow("nginx.service", c.out); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// fakeSystemctl answers `systemctl show` for nginx.service with a unit that
// starts execPath with argv, and reports every other unit as not found.
func fakeSystemctl(t *testing.T, dir, execPath, argv string, active bool) {
	t.Helper()
	state := "inactive"
	if active {
		state = "active"
	}
	writeExecutable(t, filepath.Join(dir, "systemctl"), "#!/bin/sh\n"+
		"for unit; do :; done\n"+
		"if [ \"$unit\" = nginx.service ]; then\n"+
		"  echo LoadState=loaded\n"+
		"  echo ActiveState="+state+"\n"+
		"  echo 'ExecStart={ path="+execPath+" ; argv[]="+argv+" ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }'\n"+
		"else\n"+
		"  echo LoadState=not-found\n"+
		"  echo ActiveState=inactive\n"+
		"fi\n")
}

func pretendLinux(t *testing.T) {
	t.Helper()
	saved := hostOS
	hostOS = "linux"
	t.Cleanup(func() { hostOS = saved })
}

// With several nginx installed, the one the systemd unit starts is the one
// serving the sites, even when another comes first on PATH; it may also be
// started with another main configuration file than its compiled-in one.
func TestDetectionPrefersTheSystemdUnit(t *testing.T) {
	resetNginxDetection(t)
	pretendLinux(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	served := filepath.Join(dir, "sbin", "nginx")
	writeExecutable(t, served, "#!/bin/sh\n"+
		"echo 'nginx version: nginx/1.24.0' >&2\n"+
		"echo 'configure arguments: --prefix=/usr/share/nginx --conf-path=/etc/nginx/nginx.conf --pid-path=/run/nginx.pid' >&2\n")
	writeExecutable(t, filepath.Join(bin, "nginx"), "#!/bin/sh\n"+
		"echo 'nginx version: nginx/1.27.0 (other)' >&2\n"+
		"echo 'configure arguments: --prefix=/usr/local/nginx' >&2\n")
	fakeSystemctl(t, bin, served, served+" -c /srv/nginx/main.conf", true)
	t.Setenv("PATH", bin)

	info := ReadNginxCompileInfo()
	if info.NginxExec != served || info.Version != "nginx/1.24.0" || info.NginxConfPath != "/srv/nginx/main.conf" {
		t.Fatalf("detected %+v", info)
	}
	if NginxBinary() != served {
		t.Fatalf("NginxBinary() = %q", NginxBinary())
	}
}

// A unit may start a wrapper script; running it with -V would not be nginx.
func TestDetectionIgnoresWrapperScripts(t *testing.T) {
	resetNginxDetection(t)
	pretendLinux(t)
	bin := t.TempDir()
	wrapper := filepath.Join(bin, "start-web.sh")
	writeExecutable(t, wrapper, "#!/bin/sh\nexit 1\n")
	writeExecutable(t, filepath.Join(bin, "nginx"), "#!/bin/sh\n"+
		"echo 'nginx version: nginx/1.27.0' >&2\n"+
		"echo 'configure arguments: --conf-path=/etc/nginx/nginx.conf' >&2\n")
	fakeSystemctl(t, bin, wrapper, wrapper, true)
	t.Setenv("PATH", bin)

	if got := ReadNginxCompileInfo().NginxExec; got != filepath.Join(bin, "nginx") {
		t.Fatalf("NginxExec = %q", got)
	}
}
