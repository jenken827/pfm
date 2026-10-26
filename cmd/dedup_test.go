package cmd

import "testing"

// TestResolveDedupDryRun 锁定安全默认：不加参数只预览，--execute 才真正执行
func TestResolveDedupDryRun(t *testing.T) {
	cases := []struct {
		name            string
		dryRun, execute bool
		want            bool
	}{
		{"默认只预览", true, false, true},
		{"--execute 真正执行", true, true, false},
		{"--dry-run=false 真正执行", false, false, false},
		{"--execute 优先于 --dry-run", false, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveDedupDryRun(c.dryRun, c.execute); got != c.want {
				t.Fatalf("resolveDedupDryRun(%v, %v) = %v, want %v", c.dryRun, c.execute, got, c.want)
			}
		})
	}
}

// TestDedupFlagDefaults 锁定命令行的默认值：dry-run=true、execute=false
func TestDedupFlagDefaults(t *testing.T) {
	dry := dedupCmd.Flags().Lookup("dry-run")
	if dry == nil {
		t.Fatal("缺少 --dry-run 参数")
	}
	if dry.DefValue != "true" {
		t.Fatalf("--dry-run 默认值 = %q, want true（默认必须只预览）", dry.DefValue)
	}
	exec := dedupCmd.Flags().Lookup("execute")
	if exec == nil {
		t.Fatal("缺少 --execute 参数")
	}
	if exec.DefValue != "false" {
		t.Fatalf("--execute 默认值 = %q, want false", exec.DefValue)
	}
}
