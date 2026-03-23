package cmd

import "testing"

func TestAuthPersistentFlagsAvailableOnSubcommands(t *testing.T) {
	cmd := newAuthCmd()
	for _, sub := range []string{"show", "validate", "set", "set-user", "login", "refresh", "setup", "methods"} {
		subcmd, _, err := cmd.Find([]string{sub})
		if err != nil {
			t.Fatalf("find subcommand %s: %v", sub, err)
		}
		if f := subcmd.Flag("context"); f == nil {
			t.Fatalf("expected --context flag on auth %s", sub)
		}
		if f := subcmd.Flag("config"); f == nil {
			t.Fatalf("expected --config flag on auth %s", sub)
		}
		if f := subcmd.Flag("global"); f == nil {
			t.Fatalf("expected --global flag on auth %s", sub)
		}
	}
}
