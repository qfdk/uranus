package config

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// SystemdUnit is what systemd reports about a unit.
type SystemdUnit struct {
	Name   string
	Loaded bool
	// Active reports whether systemd is running the unit. A loaded unit that
	// is inactive while nginx runs means nginx was started by hand.
	Active bool
	// ExecPath is the program the unit starts and ConfArg its -c argument.
	ExecPath string
	ConfArg  string
}

// nginxUnitNames are the units nginx is installed as: the nginx packages of
// the distributions and of nginx.org use nginx.service, OpenResty's packages
// openresty.service.
var nginxUnitNames = []string{"nginx.service", "openresty.service"}

// showSystemdUnit asks systemd about a unit. Where systemd does not run
// (macOS, containers) systemctl is missing or fails, and the unit is reported
// as not loaded.
func showSystemdUnit(name string) SystemdUnit {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "show", "-p", "LoadState", "-p", "ActiveState", "-p", "ExecStart", name).Output()
	if err != nil {
		return SystemdUnit{Name: name}
	}
	return parseSystemdShow(name, string(out))
}

// FindNginxUnit returns the systemd unit nginx is installed as, preferring
// one systemd is running.
func FindNginxUnit() (SystemdUnit, bool) {
	var loaded []SystemdUnit
	for _, name := range nginxUnitNames {
		if unit := showSystemdUnit(name); unit.Loaded {
			if unit.Active {
				return unit, true
			}
			loaded = append(loaded, unit)
		}
	}
	if len(loaded) > 0 {
		return loaded[0], true
	}
	return SystemdUnit{}, false
}

// parseSystemdShow reads the Key=Value lines printed by `systemctl show`.
func parseSystemdShow(name, out string) SystemdUnit {
	unit := SystemdUnit{Name: name}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			unit.Loaded = value == "loaded"
		case "ActiveState":
			unit.Active = value == "active" || value == "reloading"
		case "ExecStart":
			unit.ExecPath, unit.ConfArg = parseExecStart(value)
		}
	}
	return unit
}

// parseExecStart reads the program and its -c argument from an ExecStart
// value such as
// "{ path=/usr/sbin/nginx ; argv[]=/usr/sbin/nginx -c /etc/nginx/nginx.conf ; ignore_errors=no ; ... }".
func parseExecStart(value string) (path, conf string) {
	for _, field := range strings.Split(value, " ; ") {
		field = strings.TrimSpace(strings.Trim(strings.TrimSpace(field), "{}"))
		if p, ok := strings.CutPrefix(field, "path="); ok && path == "" {
			path = p
		}
		if argv, ok := strings.CutPrefix(field, "argv[]="); ok {
			args := strings.Fields(argv)
			for i := 1; i+1 < len(args); i++ {
				if args[i] == "-c" {
					conf = args[i+1]
				}
			}
		}
	}
	// nginx.org 的服务写的是 -c ${CONFFILE}，systemctl show 不展开变量
	if strings.Contains(conf, "$") {
		conf = ""
	}
	return path, conf
}
