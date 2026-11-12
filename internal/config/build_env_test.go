package config

import "testing"

func TestBuildEnvLimitsDefaultsAndOverrides(t *testing.T) {
	for _, key := range []string{"PIER_BUILD_CPUS", "PIER_BUILD_MEMORY", "PIER_BUILD_MIN_FREE_GB", "PIER_BUILD_CACHE_GB"} {
		t.Setenv(key, "")
	}
	defaults, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Build != (BuildLimits{CPUs: 1, Memory: "2g", MinFreeGB: 2, CacheGB: 10}) {
		t.Fatalf("unexpected defaults: %+v", defaults.Build)
	}
	t.Setenv("PIER_BUILD_CPUS", "0.5")
	t.Setenv("PIER_BUILD_MEMORY", "512m")
	t.Setenv("PIER_BUILD_MIN_FREE_GB", "0")
	t.Setenv("PIER_BUILD_CACHE_GB", "4")
	overrides, err := Load()
	if err != nil || overrides.Build != (BuildLimits{CPUs: 0.5, Memory: "512m", MinFreeGB: 0, CacheGB: 4}) {
		t.Fatalf("unexpected overrides: %+v %v", overrides.Build, err)
	}
}

func TestBuildEnvLimitsRejectInvalidValues(t *testing.T) {
	for _, key := range []string{"PIER_BUILD_CPUS", "PIER_BUILD_MEMORY", "PIER_BUILD_MIN_FREE_GB", "PIER_BUILD_CACHE_GB"} {
		t.Setenv(key, "")
	}
	for key, values := range map[string][]string{
		"PIER_BUILD_CPUS":        {"0", "0.001", "-1", "NaN", "+Inf", "garbage", "1025"},
		"PIER_BUILD_MEMORY":      {"0", "-1g", "1g --privileged", "nope"},
		"PIER_BUILD_MIN_FREE_GB": {"-1", "x"},
		"PIER_BUILD_CACHE_GB":    {"0", "-1", "x"},
	} {
		for _, value := range values {
			t.Run(key+"="+value, func(t *testing.T) {
				t.Setenv(key, value)
				if _, err := Load(); err == nil {
					t.Error("accepted invalid limit")
				}
			})
		}
	}
}
