package config

import (
	"testing"
	"time"
)

func TestExplicitLimitsAndDefaultDeny(t *testing.T) {
	for _, k := range []string{"SEAWEED_ZIP_TRUSTED_CIDRS", "SEAWEED_ZIP_ADMIN_CIDRS", "SEAWEED_ZIP_SOURCES"} {
		t.Setenv(k, "")
	}
	t.Setenv("SEAWEED_ZIP_MAX_OBJECTS", "7")
	t.Setenv("SEAWEED_ZIP_TASK_TIMEOUT_SECONDS", "12")
	t.Setenv("SEAWEED_ZIP_DOWNLOAD_TIMEOUT_SECONDS", "13")
	c, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if c.MaxObjects != 7 || c.TaskTimeout != 12*time.Second || c.DownloadTimeout != 13*time.Second {
		t.Fatal("limits ignored")
	}
	if len(c.Trusted) != 0 || len(c.Admin) != 0 || len(c.Sources) != 0 {
		t.Fatal("not deny by default")
	}
	t.Setenv("SEAWEED_ZIP_MAX_OBJECTS", "-1")
	if _, e = Load(); e == nil {
		t.Fatal("invalid limit accepted")
	}
}
