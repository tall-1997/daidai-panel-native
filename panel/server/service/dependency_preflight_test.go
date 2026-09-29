package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectStaticScriptDependenciesPython(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "localmod.py"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	candidates := DetectStaticScriptDependencies(".py", `import requests
from bs4 import BeautifulSoup
import json
import localmod
import requests`, workDir)
	if len(candidates) != 2 {
		t.Fatalf("expected two third-party candidates, got %+v", candidates)
	}
	got := map[string]bool{}
	for _, candidate := range candidates {
		got[candidate.PackageName] = true
	}
	if !got["requests"] || !got["beautifulsoup4"] || len(got) != 2 {
		t.Fatalf("unexpected candidates: %+v", got)
	}
}

func TestDetectStaticScriptDependenciesNode(t *testing.T) {
	candidates := DetectStaticScriptDependencies(".js", `const fs = require('fs');
const axios = require('axios');
import got from 'got';
import('node:crypto');
require('./local');
require('axios');`, t.TempDir())
	if len(candidates) != 2 {
		t.Fatalf("expected axios and got only, got %+v", candidates)
	}
	got := map[string]bool{}
	for _, candidate := range candidates {
		got[candidate.PackageName] = true
	}
	if !got["axios"] || !got["got"] || len(got) != 2 {
		t.Fatalf("unexpected candidates: %+v", got)
	}
}

func TestDetectStaticScriptDependenciesSkipsLocalNodeModule(t *testing.T) {
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, "node_modules", "axios"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectStaticScriptDependencies(".js", `require('axios')`, workDir); len(got) != 0 {
		t.Fatalf("expected installed local node module to be skipped, got %+v", got)
	}
}
