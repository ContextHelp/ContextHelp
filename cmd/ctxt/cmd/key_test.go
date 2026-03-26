package cmd

import (
	"strings"
	"testing"
)

func TestKeyCommandRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "key" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("key command not registered on rootCmd")
	}
}

func TestKeyInitSubcommandRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use != "key" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Use == "init" {
				found = true
				break
			}
		}
		break
	}
	if !found {
		t.Fatal("key init subcommand not registered")
	}
}

func TestKeyHelp(t *testing.T) {
	out, err := executeCommand("key", "--help")
	if err != nil {
		t.Fatalf("key --help: %v", err)
	}
	if !strings.Contains(out, "init") {
		t.Errorf("help output should mention 'init', got: %s", out)
	}
	if !strings.Contains(out, "rotate") {
		t.Errorf("help output should mention 'rotate', got: %s", out)
	}
}

func TestKeyRotateSubcommandRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use != "key" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Use == "rotate" {
				found = true
				break
			}
		}
		break
	}
	if !found {
		t.Fatal("key rotate subcommand not registered")
	}
}

func TestKeyRotateHelp(t *testing.T) {
	out, err := executeCommand("key", "rotate", "--help")
	if err != nil {
		t.Fatalf("key rotate --help: %v", err)
	}
	if !strings.Contains(out, "rotation") {
		t.Errorf("rotate help should mention 'rotation', got: %s", out)
	}
}

func TestConfigBackupSubcommandRegistered(t *testing.T) {
	out, err := executeCommand("config", "--help")
	if err != nil {
		t.Fatalf("config --help: %v", err)
	}
	if !strings.Contains(out, "backup") {
		t.Errorf("config help should mention 'backup', got: %s", out)
	}
}

func TestConfigRestoreSubcommandRegistered(t *testing.T) {
	out, err := executeCommand("config", "--help")
	if err != nil {
		t.Fatalf("config --help: %v", err)
	}
	if !strings.Contains(out, "restore") {
		t.Errorf("config help should mention 'restore', got: %s", out)
	}
}
