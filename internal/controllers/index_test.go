package controllers

import "testing"

func TestShortCommit(t *testing.T) {
	for id, want := range map[string]string{
		"":          "",
		"DEV888888": "DEV88888",
		"17e4e9696f35f40f57890d540301fc6f9ff76f76": "17e4e969",
	} {
		if got := shortCommit(id); got != want {
			t.Errorf("shortCommit(%q) = %q, want %q", id, got, want)
		}
	}
}

// The dashboard shows short names so that they fit next to their label; the
// full name stays in the tooltip and on the build options page.
func TestShortCPUModel(t *testing.T) {
	for model, want := range map[string]string{
		"AMD EPYC 7551 32-Core Processor":           "AMD EPYC 7551 32-Core",
		"Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz": "Intel Xeon E5-2680 v4",
		"Intel(R) Core(TM) i7-8700 CPU @ 3.20GHz":   "Intel Core i7-8700",
		"AMD Ryzen 7 5800H with Radeon Graphics":    "AMD Ryzen 7 5800H",
		"QEMU Virtual CPU version 2.5+":             "QEMU Virtual CPU version 2.5+",
		"Neoverse-N1":                               "Neoverse-N1",
		"Apple M2 Pro":                              "Apple M2 Pro",
		"":                                          "",
	} {
		if got := shortCPUModel(model); got != want {
			t.Errorf("shortCPUModel(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestShortCompiler(t *testing.T) {
	for version, want := range map[string]string{
		"gcc 11.4.0 (Ubuntu 11.4.0-1ubuntu1~22.04.3)": "gcc 11.4.0",
		"gcc 8.5.0 20210514 (Red Hat 8.5.0-20) (GCC)": "gcc 8.5.0 20210514",
		"clang 14.0.3 (clang-1403.0.22.14.1)":         "clang 14.0.3",
		"N/A":                                         "N/A",
	} {
		if got := shortCompiler(version); got != want {
			t.Errorf("shortCompiler(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestShortSSL(t *testing.T) {
	for version, want := range map[string]string{
		"OpenSSL 3.0.2 15 Mar 2022":   "OpenSSL 3.0.2",
		"OpenSSL 1.1.1f  31 Mar 2020": "OpenSSL 1.1.1f",
		"LibreSSL 3.8.2":              "LibreSSL 3.8.2",
		"BoringSSL":                   "BoringSSL",
		"N/A":                         "N/A",
	} {
		if got := shortSSL(version); got != want {
			t.Errorf("shortSSL(%q) = %q, want %q", version, got, want)
		}
	}
}
