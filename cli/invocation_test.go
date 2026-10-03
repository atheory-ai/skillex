package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvocationDetection(t *testing.T) {
	for _, tc := range []struct{ name, pkg, file, want string }{
		{"global", `{}`, "", "global"},
		{"npm", `{"devDependencies":{"@atheory-ai/skillex":"1.0"}}`, "package-lock.json", "npm"},
		{"pnpm", `{"devDependencies":{"@atheory-ai/skillex":"1.0"},"packageManager":"pnpm@10.0"}`, "", "pnpm"},
		{"classic", `{"devDependencies":{"@atheory-ai/skillex":"1.0"},"packageManager":"yarn@1.22.0"}`, "", "yarn-classic"},
		{"berry", `{"devDependencies":{"@atheory-ai/skillex":"1.0"}}`, ".yarnrc.yml", "yarn-berry"},
		{"no dependency", `{"packageManager":"pnpm@10.0"}`, "pnpm-lock.yaml", "global"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			os.WriteFile(filepath.Join(root, "package.json"), []byte(tc.pkg), 0o644)
			if tc.file != "" {
				os.WriteFile(filepath.Join(root, tc.file), nil, 0o644)
			}
			if got := detectInvocation(root); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestChooseInvocation(t *testing.T) {
	for _, answer := range []string{"\n", "npm\n", "bogus\n"} {
		var out bytes.Buffer
		got, err := chooseInvocation(strings.NewReader(answer), &out, "pnpm")
		if answer == "bogus\n" {
			if err == nil {
				t.Fatal("invalid choice accepted")
			}
			continue
		}
		want := "pnpm"
		if answer == "npm\n" {
			want = "npm"
		}
		if err != nil || got != want {
			t.Fatalf("%s %v", got, err)
		}
	}
}
