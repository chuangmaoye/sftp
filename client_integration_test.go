package sftp

// sftp integration tests
// enable with -integration

import (
	"flag"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"testing"

	"github.com/kr/fs"
)

const (
	READONLY  = true
	READWRITE = false

	debuglevel = "ERROR" // set to "DEBUG" for debugging
)

var testIntegration = flag.Bool("integration", false, "perform integration tests against sftp server process")

// testClient returns a *Client connected to a localy running sftp-server
// the *exec.Cmd returned must be defer Wait'd.
func testClient(t *testing.T, readonly bool) (*Client, *exec.Cmd) {
	if !*testIntegration {
		t.Skip("skipping intergration test")
	}
	cmd := exec.Command("/usr/lib/openssh/sftp-server", "-e", "-R", "-l", debuglevel) // log to stderr, read only
	if !readonly {
		cmd = exec.Command("/usr/lib/openssh/sftp-server", "-e", "-l", debuglevel) // log to stderr
	}
	cmd.Stderr = os.Stdout
	pw, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	pr, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("could not start sftp-server process: %v", err)
	}
	sftp := &Client{
		w: pw,
		r: pr,
	}
	if err := sftp.sendInit(); err != nil {
		defer cmd.Wait()
		t.Fatal(err)
	}
	if err := sftp.recvVersion(); err != nil {
		defer cmd.Wait()
		t.Fatal(err)
	}
	return sftp, cmd
}

func TestNewClient(t *testing.T) {
	sftp, cmd := testClient(t, READONLY)
	defer cmd.Wait()

	if err := sftp.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClientLstat(t *testing.T) {
	sftp, cmd := testClient(t, READONLY)
	defer cmd.Wait()
	defer sftp.Close()

	f, err := ioutil.TempFile("", "sftptest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	want, err := os.Lstat(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	got, err := sftp.Lstat(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	if !sameFile(want, got) {
		t.Fatalf("Lstat(%q): want %#v, got %#v", f.Name(), want, got)
	}
}

func TestClientLstatMissing(t *testing.T) {
	sftp, cmd := testClient(t, READONLY)
	defer cmd.Wait()
	defer sftp.Close()

	f, err := ioutil.TempFile("", "sftptest")
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(f.Name())

	_, err = sftp.Lstat(f.Name())
	if err1, ok := err.(*StatusError); !ok || err1.Code != ssh_FX_NO_SUCH_FILE {
		t.Fatalf("Lstat: want: %v, got %#v", ssh_FX_NO_SUCH_FILE, err)
	}
}

func TestClientOpen(t *testing.T) {
	sftp, cmd := testClient(t, READONLY)
	defer cmd.Wait()
	defer sftp.Close()

	f, err := ioutil.TempFile("", "sftptest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	got, err := sftp.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClientRead(t *testing.T) {
	sftp, cmd := testClient(t, READONLY)
	defer cmd.Wait()
	defer sftp.Close()

	f, err := ioutil.TempFile("", "sftptest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString("Hello world!"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := sftp.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()

	b, err := ioutil.ReadAll(got)
	if err != nil {
		t.Fatal(err)
	}

	if want, got := "Hello world!", string(b); got != want {
		t.Fatalf("Read(): want %q, got %q", want, got)
	}
}

var readAtTests = []struct {
	s    string
	at   int64
	want string
	err  error
}{
	{"Hello world!", 6, "world!", nil},
	{"Hello world!", 0, "Hello world!", nil},
	{"Hello world!", 12, "", io.EOF},
}

func TestClientReadAt(t *testing.T) {
	sftp, cmd := testClient(t, READONLY)
	defer cmd.Wait()
	defer sftp.Close()

	for _, tt := range readAtTests {
		f, err := ioutil.TempFile("", "sftptest")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(f.Name())

		if _, err := f.WriteString(tt.s); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}

		got, err := sftp.Open(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		defer got.Close()

		var b = make([]byte, 100)
		n, err := got.ReadAt(b, tt.at)
		b = b[:n]

		if want, got := tt.want, string(b); got != want || tt.err != err {
			t.Fatalf("Read(): want %q %v, got %q %v", want, tt.err, got, err)
		}
	}
}

func TestClientCreate(t *testing.T) {
	sftp, cmd := testClient(t, READWRITE)
	defer cmd.Wait()
	defer sftp.Close()

	f, err := ioutil.TempFile("", "sftptest")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	defer os.Remove(f.Name())

	f2, err := sftp.Create(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()
}

func TestClientCreateFailed(t *testing.T) {
	sftp, cmd := testClient(t, READONLY)
	defer cmd.Wait()
	defer sftp.Close()

	f, err := ioutil.TempFile("", "sftptest")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	defer os.Remove(f.Name())

	f2, err := sftp.Create(f.Name())
	if err1, ok := err.(*StatusError); !ok || err1.Code != ssh_FX_PERMISSION_DENIED {
		t.Fatalf("Create: want: %v, got %#v", ssh_FX_PERMISSION_DENIED, err)
	}
	if err == nil {
		f2.Close()
	}
}

func TestClientFileStat(t *testing.T) {
	sftp, cmd := testClient(t, READONLY)
	defer cmd.Wait()
	defer sftp.Close()

	f, err := ioutil.TempFile("", "sftptest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	want, err := os.Lstat(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	f2, err := sftp.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	got, err := f2.Stat()
	if err != nil {
		t.Fatal(err)
	}

	if !sameFile(want, got) {
		t.Fatalf("Lstat(%q): want %#v, got %#v", f.Name(), want, got)
	}
}

func TestClientRemove(t *testing.T) {
	sftp, cmd := testClient(t, READWRITE)
	defer cmd.Wait()
	defer sftp.Close()

	f, err := ioutil.TempFile("", "sftptest")
	if err != nil {
		t.Fatal(err)
	}
	if err := sftp.Remove(f.Name()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(f.Name()); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestClientRemoveFailed(t *testing.T) {
	sftp, cmd := testClient(t, READONLY)
	defer cmd.Wait()
	defer sftp.Close()

	f, err := ioutil.TempFile("", "sftptest")
	if err != nil {
		t.Fatal(err)
	}
	if err := sftp.Remove(f.Name()); err == nil {
		t.Fatalf("Remove(%v): want: permission denied, got %v", f.Name(), err)
	}
	if _, err := os.Lstat(f.Name()); err != nil {
		t.Fatal(err)
	}
}

func TestClientRename(t *testing.T) {
	sftp, cmd := testClient(t, READWRITE)
	defer cmd.Wait()
	defer sftp.Close()

	f, err := ioutil.TempFile("", "sftptest")
	if err != nil {
		t.Fatal(err)
	}
	f2 := f.Name() + ".new"
	if err := sftp.Rename(f.Name(), f2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(f.Name()); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(f2); err != nil {
		t.Fatal(err)
	}
}

func sameFile(want, got os.FileInfo) bool {
	return want.Name() == got.Name() &&
		want.Size() == got.Size()
}

var clientWriteTests = []struct {
	n     int
	total int64 // cumulative file size
}{
	{0, 0},
	{1, 1},
	{0, 1},
	{999, 1000},
	{24, 1024},
	{1023, 2047},
	{2048, 4095},
	{1 << 12, 8191},
	{1 << 13, 16383},
	{1 << 14, 32767},
	{1 << 15, 65535},
	{1 << 16, 131071},
	{1 << 17, 262143},
	{1 << 18, 524287},
	{1 << 19, 1048575},
	{1 << 20, 2097151},
	{1 << 21, 4194303},
}

func TestClientWrite(t *testing.T) {
	sftp, cmd := testClient(t, READWRITE)
	defer cmd.Wait()
	defer sftp.Close()

	d, err := ioutil.TempDir("", "sftptest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)

	f := path.Join(d, "writeTest")
	w, err := sftp.Create(f)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for _, tt := range clientWriteTests {
		got, err := w.Write(make([]byte, tt.n))
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.n {
			t.Errorf("Write(%v): wrote: want: %v, got %v", tt.n, tt.n, got)
		}
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if total := fi.Size(); total != tt.total {
			t.Errorf("Write(%v): size: want: %v, got %v", tt.n, tt.total, total)
		}
	}
}

// taken from github.com/kr/fs/walk_test.go

type PathTest struct {
	path, result string
}

type Node struct {
	name    string
	entries []*Node // nil if the entry is a file
	mark    int
}

var tree = &Node{
	"testdata",
	[]*Node{
		{"a", nil, 0},
		{"b", []*Node{}, 0},
		{"c", nil, 0},
		{
			"d",
			[]*Node{
				{"x", nil, 0},
				{"y", []*Node{}, 0},
				{
					"z",
					[]*Node{
						{"u", nil, 0},
						{"v", nil, 0},
					},
					0,
				},
			},
			0,
		},
	},
	0,
}

func walkTree(n *Node, path string, f func(path string, n *Node)) {
	f(path, n)
	for _, e := range n.entries {
		walkTree(e, filepath.Join(path, e.name), f)
	}
}

func makeTree(t *testing.T) {
	walkTree(tree, tree.name, func(path string, n *Node) {
		if n.entries == nil {
			fd, err := os.Create(path)
			if err != nil {
				t.Errorf("makeTree: %v", err)
				return
			}
			fd.Close()
		} else {
			os.Mkdir(path, 0770)
		}
	})
}

func markTree(n *Node) { walkTree(n, "", func(path string, n *Node) { n.mark++ }) }

func checkMarks(t *testing.T, report bool) {
	walkTree(tree, tree.name, func(path string, n *Node) {
		if n.mark != 1 && report {
			t.Errorf("node %s mark = %d; expected 1", path, n.mark)
		}
		n.mark = 0
	})
}

// Assumes that each node name is unique. Good enough for a test.
// If clear is true, any incoming error is cleared before return. The errors
// are always accumulated, though.
func mark(path string, info os.FileInfo, err error, errors *[]error, clear bool) error {
	if err != nil {
		*errors = append(*errors, err)
		if clear {
			return nil
		}
		return err
	}
	name := info.Name()
	walkTree(tree, tree.name, func(path string, n *Node) {
		if n.name == name {
			n.mark++
		}
	})
	return nil
}

func TestClientWalk(t *testing.T) {
	sftp, cmd := testClient(t, READONLY)
	defer cmd.Wait()
	defer sftp.Close()

	makeTree(t)
	errors := make([]error, 0, 10)
	clear := true
	markFn := func(walker *fs.Walker) (err error) {
		for walker.Step() {
			err = mark(walker.Path(), walker.Stat(), walker.Err(), &errors, clear)
			if err != nil {
				break
			}
		}
		return err
	}
	// Expect no errors.
	err := markFn(sftp.Walk(tree.name))
	if err != nil {
		t.Fatalf("no error expected, found: %s", err)
	}
	if len(errors) != 0 {
		t.Fatalf("unexpected errors: %s", errors)
	}
	checkMarks(t, true)
	errors = errors[0:0]

	// Test permission errors.  Only possible if we're not root
	// and only on some file systems (AFS, FAT).  To avoid errors during
	// all.bash on those file systems, skip during go test -short.
	if os.Getuid() > 0 && !testing.Short() {
		// introduce 2 errors: chmod top-level directories to 0
		os.Chmod(filepath.Join(tree.name, tree.entries[1].name), 0)
		os.Chmod(filepath.Join(tree.name, tree.entries[3].name), 0)

		// 3) capture errors, expect two.
		// mark respective subtrees manually
		markTree(tree.entries[1])
		markTree(tree.entries[3])
		// correct double-marking of directory itself
		tree.entries[1].mark--
		tree.entries[3].mark--
		err := markFn(sftp.Walk(tree.name))
		if err != nil {
			t.Fatalf("expected no error return from Walk, got %s", err)
		}
		if len(errors) != 2 {
			t.Errorf("expected 2 errors, got %d: %s", len(errors), errors)
		}
		// the inaccessible subtrees were marked manually
		checkMarks(t, true)
		errors = errors[0:0]

		// 4) capture errors, stop after first error.
		// mark respective subtrees manually
		markTree(tree.entries[1])
		markTree(tree.entries[3])
		// correct double-marking of directory itself
		tree.entries[1].mark--
		tree.entries[3].mark--
		clear = false // error will stop processing
		err = markFn(sftp.Walk(tree.name))
		if err == nil {
			t.Fatalf("expected error return from Walk")
		}
		if len(errors) != 1 {
			t.Errorf("expected 1 error, got %d: %s", len(errors), errors)
		}
		// the inaccessible subtrees were marked manually
		checkMarks(t, false)
		errors = errors[0:0]

		// restore permissions
		os.Chmod(filepath.Join(tree.name, tree.entries[1].name), 0770)
		os.Chmod(filepath.Join(tree.name, tree.entries[3].name), 0770)
	}

	// cleanup
	if err := os.RemoveAll(tree.name); err != nil {
		t.Errorf("removeTree: %v", err)
	}
}
