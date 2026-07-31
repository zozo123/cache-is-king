package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoctor(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "Dockerfile"), []byte("FROM node:latest\nCOPY . .\nRUN npm ci\n"), 0644)
	r, e := analyze(d)
	if e != nil || !high(r.Findings) {
		t.Fatalf("expected high: %#v %v", r.Findings, e)
	}
}
func TestPhase(t *testing.T) {
	if phase("exporting to docker image format") != "export" || phase("load build context") != "context" {
		t.Fatal("bad phase")
	}
}
