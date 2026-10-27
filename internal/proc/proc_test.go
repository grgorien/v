package proc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseNetUnix(t *testing.T) {
	in := `Num       RefCount Protocol Flags    Type St Inode Path
	0000000000000000: 00000002 00000000 00010000 0001 01 12345 /tmp/a.sock
	0000000000000000: 00000002 00000000 00000000 0001 01 12346 /tmp/notlisten.sock
	0000000000000000: 00000002 00000000 00010000 0001 01 12347 /tmp/with space.sock
	0000000000000000: 00000002 00000000 00010000 0001 01 12348
	`
	got, err := ParseNetUnix(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d rows", len(got))
	}
	if !got[0].Listening || got[0].Path != "/tmp/a.sock" || got[0].Inode != 12345 {
		t.Fatalf("row0: %#v", got[0])
	}
	if got[1].Listening {
		t.Fatalf("row1 should not listen")
	}
	if got[2].Path != "/tmp/with space.sock" {
		t.Fatalf("row2: %#v", got[2])
	}
	if got[3].Path != "" {
		t.Fatalf("row3: %#v", got[3])
	}
}

func TestParseNetTCP(t *testing.T) {
	in := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
	0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 11111 1
	1: 0100007F:0050 0100007F:9999 01 00000000:00000000 00:00000000 00000000  1000        0 22222 1
	`
	got, err := ParseNetTCP(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Port != 8080 || got[0].Inode != 11111 {
		t.Fatalf("got %#v", got[0])
	}
}

func TestParseStatPPid(t *testing.T) {
	stat := "123 (a b) c) S 456 789 0 0 -1"
	pp, err := ParseStatPPid(stat)
	if err != nil {
		t.Fatal(err)
	}
	if pp != 456 {
		t.Fatalf("got %d", pp)
	}
}

func TestSnapshot(t *testing.T) {
	root := t.TempDir()
	mustMkdir := func(p string) {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustSymlink := func(target, link string) {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	// proc 100 = nvim owning unix inode 500 and tcp inode 600.
	mustMkdir(filepath.Join(root, "100", "fd"))
	mustWrite(filepath.Join(root, "100", "comm"), "nvim\n")
	mustWrite(filepath.Join(root, "100", "stat"), "100 (nvim) S 1 0 0\n")
	mustSymlink("socket:[500]", filepath.Join(root, "100", "fd", "3"))
	mustSymlink("socket:[600]", filepath.Join(root, "100", "fd", "4"))
	// proc 200 = bash child of 100 owning tcp inode 601.
	mustMkdir(filepath.Join(root, "200", "fd"))
	mustWrite(filepath.Join(root, "200", "comm"), "bash\n")
	mustWrite(filepath.Join(root, "200", "stat"), "200 (bash) S 100 0 0\n")
	mustSymlink("socket:[601]", filepath.Join(root, "200", "fd", "3"))
	// proc 300 = other.
	mustMkdir(filepath.Join(root, "300", "fd"))
	mustWrite(filepath.Join(root, "300", "comm"), "other\n")
	mustWrite(filepath.Join(root, "300", "stat"), "300 (other) S 1 0 0\n")
	mustSymlink("socket:[700]", filepath.Join(root, "300", "fd", "3"))

	mustMkdir(filepath.Join(root, "net"))
	mustWrite(filepath.Join(root, "net", "unix"),
	"Num RefCount Protocol Flags Type St Inode Path\n"+
	"0: 0 0 00010000 0001 01 500 /tmp/n.sock\n"+
	"0: 0 0 00010000 0001 01 700 /tmp/o.sock\n")
	mustWrite(filepath.Join(root, "net", "tcp"),
	"sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n"+
	"0: 0100007F:1F90 00000000:0000 0A 0:0 00:0 0 0 0 600 1\n"+
	"1: 0100007F:0051 00000000:0000 0A 0:0 00:0 0 0 0 601 1\n"+
	"2: 0100007F:0052 00000000:0000 0A 0:0 00:0 0 0 0 700 1\n")
	mustWrite(filepath.Join(root, "net", "tcp6"),
	"sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n")

	snap, err := Take(root)
	if err != nil {
		t.Fatal(err)
	}
	nv := snap.NvimSockets()
	if v, ok := nv["/tmp/n.sock"]; !ok || v != 100 {
		t.Fatalf("NvimSockets: %#v", nv)
	}
	if _, ok := nv["/tmp/o.sock"]; ok {
		t.Fatalf("should not include other process socket")
	}
	desc := snap.Descendants(100)
	if !reflect.DeepEqual(desc, []int{100, 200}) {
		t.Fatalf("descendants: %#v", desc)
	}
	ports := snap.PortsFor(100)
	if !reflect.DeepEqual(ports, []int{81, 8080}) {
		t.Fatalf("ports: %#v", ports)
	}
}
