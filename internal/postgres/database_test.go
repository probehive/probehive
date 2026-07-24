package postgres

import (
	"testing"
	"testing/fstest"
)

func TestLoadMigrationsRequiresSequentialVersions(t *testing.T) {
	tests := []struct {
		name      string
		files     fstest.MapFS
		wantError bool
		wantNames []string
	}{
		{
			name: "ordered by numeric version",
			files: fstest.MapFS{
				"migrations/0002_second.sql": {Data: []byte("SELECT 2")},
				"migrations/0001_first.sql":  {Data: []byte("SELECT 1")},
			},
			wantNames: []string{"0001_first.sql", "0002_second.sql"},
		},
		{
			name: "gap rejected",
			files: fstest.MapFS{
				"migrations/0001_first.sql": {Data: []byte("SELECT 1")},
				"migrations/0003_third.sql": {Data: []byte("SELECT 3")},
			},
			wantError: true,
		},
		{
			name: "duplicate rejected",
			files: fstest.MapFS{
				"migrations/0001_first.sql": {Data: []byte("SELECT 1")},
				"migrations/0001_again.sql": {Data: []byte("SELECT 1")},
			},
			wantError: true,
		},
		{
			name: "empty rejected",
			files: fstest.MapFS{
				"migrations/readme.txt": {Data: []byte("not SQL")},
			},
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			migrations, err := loadMigrations(test.files)
			if test.wantError {
				if err == nil {
					t.Fatal("loadMigrations() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("loadMigrations() error = %v", err)
			}
			if len(migrations) != len(test.wantNames) {
				t.Fatalf("loadMigrations() length = %d, want %d", len(migrations), len(test.wantNames))
			}
			for index, name := range test.wantNames {
				if migrations[index].name != name {
					t.Errorf("migration %d name = %q, want %q", index, migrations[index].name, name)
				}
			}
		})
	}
}
