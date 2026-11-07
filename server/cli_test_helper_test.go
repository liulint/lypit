package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A native helper executable on both platforms: tests need no sh, Bash, Node or real AI account.
func init() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if !strings.HasPrefix(filepath.Base(exe), "lypit-test-cli") {
		return
	}
	scenario, err := os.ReadFile(exe + ".scenario")
	if err != nil {
		return
	}
	prompt, _ := io.ReadAll(os.Stdin)
	_ = os.WriteFile("prompt.txt", prompt, 0600)
	_ = os.WriteFile("args.txt", []byte(strings.Join(os.Args[1:], "\n")), 0600)
	f, _ := os.OpenFile("runs", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if f != nil {
		_, _ = f.WriteString("run")
		f.Close()
	}
	output := ""
	for i, arg := range os.Args {
		if arg == "--output-last-message" && i+1 < len(os.Args) {
			output = os.Args[i+1]
		}
	}
	var result []byte
	switch string(scenario) {
	case "tree":
		if os.Getenv("LYPIT_TEST_CHILD") == "1" {
			_ = os.WriteFile("child.pid", []byte(strconv.Itoa(os.Getpid())), 0600)
		} else {
			child := exec.Command(exe)
			child.Env = append(os.Environ(), "LYPIT_TEST_CHILD=1")
			if child.Start() != nil {
				os.Exit(6)
			}
		}
		for {
			time.Sleep(time.Second)
		}
	case "fail":
		_, _ = os.Stderr.WriteString("CLI diagnostic: inference failed\nAuthorization: Bearer " + os.Getenv("LYPIT_TEST_SECRET_TOKEN") + "\n")
		os.Exit(1)
	case "reference-limit":
		_, _ = os.Stderr.WriteString("ERROR tools::router: `referenced_image_paths` must contain at most 5 paths\n")
		result = []byte(`{"source":""}`)
	case "empty-image":
		_, _ = os.Stderr.WriteString("image_gen unavailable: diagnostic fixture\n")
		result = []byte(`{"source":""}`)
	case "text-usage":
		_, _ = os.Stdout.WriteString("{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":120,\"output_tokens\":10,\"cached_input_tokens\":100}}\n")
		result = []byte(`{"ok":true}`)
	case "text":
		result = []byte(`{"ok":true}`)
	case "claude":
		_, _ = os.Stdout.Write([]byte(`{"result":"{\"ok\":true}","is_error":false}`))
		os.Exit(0)
	case "analyze":
		result = []byte(`{"script":"【0—60秒｜主场景】\n主角看向窗外。","assets":[{"kind":"character","name":"主角","description":"人物设定"},{"kind":"scene","name":"主场景","description":"场景设定"}]}`)
	case "image":
		b, err := os.ReadFile(os.Getenv("IMAGE_FIXTURE"))
		if err != nil {
			os.Exit(2)
		}
		file := filepath.Join(os.Getenv("CODEX_HOME"), "generated_images", "current.png")
		if os.WriteFile(file, b, 0600) != nil {
			os.Exit(3)
		}
		result, _ = json.Marshal(cliImageReceipt{Source: file})
	default:
		os.Exit(4)
	}
	if os.WriteFile(output, result, 0600) != nil {
		os.Exit(5)
	}
	os.Exit(0)
}

func copyTestFile(source, dest string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func fakeCLI(t *testing.T, scenario string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "lypit-test-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	file := filepath.Join(t.TempDir(), name)
	if err = os.Link(exe, file); err != nil {
		if err = copyTestFile(exe, file, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(file+".scenario", []byte(scenario), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}
