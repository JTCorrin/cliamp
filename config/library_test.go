package config

import (
	"reflect"
	"testing"
)

func TestLibraryConfig(t *testing.T) {
	tests := []struct {
		name    string
		section string
		want    LibraryConfig
		isSet   bool
	}{
		{"absent", "", LibraryConfig{}, false},
		{"header only", "[library]\n", LibraryConfig{}, false},
		{"one path", "[library]\npaths = [\"/mnt/nas/music\"]\n", LibraryConfig{Paths: []string{"/mnt/nas/music"}}, true},
		{"two paths", "[library]\npaths = /a, /b\n", LibraryConfig{Paths: []string{"/a", "/b"}}, true},
		{"workers", "[library]\npaths = [\"/a\"]\nworkers = 12\n", LibraryConfig{Paths: []string{"/a"}, Workers: 12}, true},
		{"zero workers ignored", "[library]\npaths = [\"/a\"]\nworkers = 0\n", LibraryConfig{Paths: []string{"/a"}}, true},
		{"bad workers ignored", "[library]\npaths = [\"/a\"]\nworkers = many\n", LibraryConfig{Paths: []string{"/a"}}, true},
		{"scan off", "[library]\npaths = [\"/a\"]\nscan_on_start = false\n", LibraryConfig{Paths: []string{"/a"}, ScanDisabled: true}, true},
		{"scan on", "[library]\npaths = [\"/a\"]\nscan_on_start = true\n", LibraryConfig{Paths: []string{"/a"}}, true},
		{"bad scan value ignored", "[library]\npaths = [\"/a\"]\nscan_on_start = maybe\n", LibraryConfig{Paths: []string{"/a"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loadConfigText(t, tt.section).Library
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Library = %+v, want %+v", got, tt.want)
			}
			if got.IsSet() != tt.isSet {
				t.Errorf("IsSet() = %v, want %v", got.IsSet(), tt.isSet)
			}
		})
	}
}
