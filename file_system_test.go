// Copyright 2015 Muir Manders.  All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package goftp

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestDelete(t *testing.T) {
	for _, addr := range ftpdAddrs {
		c, err := DialConfig(Config{User: "goftp", Password: "rocks"}, addr)
		if err != nil {
			t.Fatal(err)
		}

		os.Remove("testroot/git-ignored/foo")

		err = c.Store("git-ignored/foo", bytes.NewReader([]byte{1, 2, 3, 4}))
		if err != nil {
			t.Fatal(err)
		}

		_, err = os.Open("testroot/git-ignored/foo")
		if err != nil {
			t.Fatal("file is not there?", err)
		}

		if err := c.Delete("git-ignored/foo"); err != nil {
			t.Error(err)
		}

		if err := c.Delete("git-ignored/foo"); err == nil {
			t.Error("should be some sort of errorg")
		}

		if c.numOpenConns() != len(c.freeConnCh) {
			t.Error("Leaked a connection")
		}
	}
}

func TestRename(t *testing.T) {
	for _, addr := range ftpdAddrs {
		c, err := DialConfig(Config{User: "goftp", Password: "rocks"}, addr)
		if err != nil {
			t.Fatal(err)
		}

		os.Remove("testroot/git-ignored/foo")

		err = c.Store("git-ignored/foo", bytes.NewReader([]byte{1, 2, 3, 4}))
		if err != nil {
			t.Fatal(err)
		}

		_, err = os.Open("testroot/git-ignored/foo")
		if err != nil {
			t.Fatal("file is not there?", err)
		}

		if err := c.Rename("git-ignored/foo", "git-ignored/bar"); err != nil {
			t.Error(err)
		}

		newContents, err := ioutil.ReadFile("testroot/git-ignored/bar")
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(newContents, []byte{1, 2, 3, 4}) {
			t.Error("file contents wrong", newContents)
		}

		if c.numOpenConns() != len(c.freeConnCh) {
			t.Error("Leaked a connection")
		}
	}
}

func TestMkdirRmdir(t *testing.T) {
	for _, addr := range ftpdAddrs {
		c, err := DialConfig(Config{User: "goftp", Password: "rocks"}, addr)
		if err != nil {
			t.Fatal(err)
		}

		os.Remove("testroot/git-ignored/foodir")

		_, err = c.Mkdir("git-ignored/foodir")
		if err != nil {
			t.Fatal(err)
		}

		stat, err := os.Stat("testroot/git-ignored/foodir")
		if err != nil {
			t.Fatal(err)
		}

		if !stat.IsDir() {
			t.Error("should be a dir")
		}

		err = c.Rmdir("git-ignored/foodir")
		if err != nil {
			t.Fatal(err)
		}

		_, err = os.Stat("testroot/git-ignored/foodir")
		if !os.IsNotExist(err) {
			t.Error("directory should be gone")
		}

		cwd, err := c.Getwd()
		if err != nil {
			t.Fatal(err)
		}

		os.Remove(`testroot/git-ignored/dir-with-"`)
		dir, err := c.Mkdir(`git-ignored/dir-with-"`)
		if dir != `git-ignored/dir-with-"` && dir != path.Join(cwd, `git-ignored/dir-with-"`) {
			t.Errorf("Unexpected dir-with-quote value: %s", dir)
		}

		if c.numOpenConns() != len(c.freeConnCh) {
			t.Error("Leaked a connection")
		}
	}
}

func mustParseTime(f, s string) time.Time {
	t, err := time.Parse(timeFormat, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseMLST(t *testing.T) {
	cases := []struct {
		raw string
		exp *ftpFile
	}{
		{
			// dirs dont necessarily have size
			"modify=19991014192630;perm=fle;type=dir;unique=806U246E0B1;UNIX.group=1;UNIX.mode=0755;UNIX.owner=0; files",
			&ftpFile{
				name:  "files",
				mtime: mustParseTime(timeFormat, "19991014192630"),
				mode:  os.FileMode(0755) | os.ModeDir,
			},
		},
		{
			// xlightftp (windows ftp server) mlsd output I found
			"size=1089207168;type=file;modify=20090426141232; adsl TV 2009-04-22 23-55-05 Jazz Icons   Lionel Hampton Live in 1958 [Mezzo].avi",
			&ftpFile{
				name:  "adsl TV 2009-04-22 23-55-05 Jazz Icons   Lionel Hampton Live in 1958 [Mezzo].avi",
				mtime: mustParseTime(timeFormat, "20090426141232"),
				mode:  os.FileMode(0400),
				size:  1089207168,
			},
		},
		{
			// test "type=OS.unix=slink"
			"type=OS.unix=slink:;size=32;modify=20140728100902;UNIX.mode=0777;UNIX.uid=647;UNIX.gid=649;unique=fd01g1220c04; access-logs",
			&ftpFile{
				name:  "access-logs",
				mtime: mustParseTime(timeFormat, "20140728100902"),
				mode:  os.FileMode(0777) | os.ModeSymlink,
				size:  32,
			},
		},
		{
			// test "type=OS.unix=symlink"
			"modify=20150928140340;perm=adfrw;size=6;type=OS.unix=symlink;unique=801U5AA227;UNIX.group=1000;UNIX.mode=0777;UNIX.owner=1000; slinkdir",
			&ftpFile{
				name:  "slinkdir",
				mtime: mustParseTime(timeFormat, "20150928140340"),
				mode:  os.FileMode(0777) | os.ModeSymlink,
				size:  6,
			},
		},
	}

	var parser mlstParser
	for _, c := range cases {
		c.exp.raw = c.raw

		got, err := parser.parse(c.raw, false)
		if err != nil {
			t.Fatal(err)
		}
		gotFile := got.(*ftpFile)
		if !reflect.DeepEqual(gotFile, c.exp) {
			t.Errorf("exp %+v\n got %+v", c.exp, gotFile)
		}
	}
}

var mlstCases = []string{
	"modify=20160513014228;perm=adfrw;size=399;type=file;unique=FD00U29043978;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; 408.php",
	"modify=20180407164538;perm=adfrw;size=381514;type=file;unique=FD00U4565E18;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; browscap.ini",
	"modify=20170806081452;perm=adfrw;size=10647;type=file;unique=FD00UDD0EA24;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; codepress-admin-columns-da_DK.mo",
	"modify=20170806081450;perm=flcdmpe;type=pdir;unique=FD00U1064255A;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; ..",
	"modify=20141028200222;perm=adfrw;size=173;type=file;unique=FD00UE28BBA7;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; icon_smile.gif",
	"modify=20171108093751;perm=adfrw;size=1032;type=file;unique=FD00U831DA85;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; admin.php",
	"modify=20171108093751;perm=adfrw;size=34477;type=file;unique=FD00UC3F897C;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; Browscap.php",
	"modify=20170806081458;perm=flcdmpe;type=cdir;unique=FD00U3832DF43;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; .",
	"modify=20180312093921;perm=adfrw;size=31649;type=file;unique=FD00U2086752A;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; Capture-2-150x150.png",
	"modify=20170806081450;perm=adfrw;size=4050;type=file;unique=FD00U30209EB7;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; API.php",
	"modify=20170608112954;perm=flcdmpe;type=cdir;unique=FD00U4651AD9;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; .",
	"modify=20150708111544;perm=adfrw;size=513;type=file;unique=FD00U2C498E59;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; README_License.txt",
	"modify=20171205110148;perm=flcdmpe;type=dir;unique=FD00U28B52AC5;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; font",
	"modify=20170608112954;perm=flcdmpe;type=cdir;unique=FD00U859BABC;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; .",
	"modify=20170730062048;perm=adfrw;size=308;type=file;unique=FD00U3831C8D1;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; autoload_psr4.php",
	"modify=20170730062038;perm=flcdmpe;type=dir;unique=FD00U18041F43;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; build",
	"modify=20171205110148;perm=flcdmpe;type=dir;unique=FD00U3C41E09D;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; ajax",
	"modify=20170806081452;perm=flcdmpe;type=cdir;unique=FD00U849905C;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; .",
	"modify=20171220155012;perm=flcdmpe;type=dir;unique=FD00U2C08F69C;UNIX.group=1170;UNIX.mode=0775;UNIX.owner=1168; lib",
	"modify=20180313084927;perm=adfrw;size=8763;type=file;unique=FD00UD5A3103;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; tali-278x180.jpg",
	"modify=20170806081456;perm=adfrw;size=53585;type=file;unique=FD00U21FDC1;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; screenshot-4.png",
	"modify=20180312093924;perm=adfrw;size=183356;type=file;unique=FD00U8046AB2;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; Capture-619x425.png",
	"modify=20180401110016;perm=adfrw;size=30267;type=file;unique=FD00U381238D8;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; 404-300x60.png",
	"modify=20170806081450;perm=flcdmpe;type=dir;unique=FD00UC42F92C;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; admin",
	"modify=20170608112954;perm=flcdmpe;type=dir;unique=FD00U306C3509;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; lists",
	"modify=20171205110149;perm=flcdmpe;type=dir;unique=FD00U24045ECA;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; debug",
	"modify=20180312093913;perm=adfrw;size=81548;type=file;unique=FD00U1D94DB68;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; happy-israel-752x582.jpg",
	"modify=20170730062044;perm=flcdmpe;type=dir;unique=FD00U30209E92;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; advanced_file",
	"modify=20170806081450;perm=flcdmpe;type=pdir;unique=FD00U1064255A;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; ..",
	"modify=20170723104024;perm=adfrw;size=27868;type=file;unique=FD00U1C23D27F;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; jquery-ui-1.7.2.custom.css",
	"modify=20170730062046;perm=adfrw;size=9790;type=file;unique=FD00U1A4362;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; ajax.php",
	"modify=20160927123930;perm=adfrw;size=277;type=file;unique=FD00U38056F01;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; theme-editor.php",
	"modify=20170519142744;perm=adfrw;size=16541;type=file;unique=FD00U2C0701A1;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; dashboard.js",
	"modify=20170608112954;perm=flcdmpe;type=pdir;unique=FD00U29043942;UNIX.group=1170;UNIX.mode=0755;UNIX.owner=1168; ..",
	"modify=20170730062046;perm=adfrw;size=17448;type=file;unique=FD00UDCB2137;UNIX.group=1170;UNIX.mode=0644;UNIX.owner=1168; caldera-forms-de_DE.mo",
}

func BenchmarkParseMLST(b *testing.B) {
	for n := 0; n < b.N; n++ {
		for _, c := range mlstCases {
			_, err := parseMLST(c, false)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func compareFileInfos(a, b os.FileInfo) error {
	if a.Name() != b.Name() {
		return fmt.Errorf("Name(): %s != %s", a.Name(), b.Name())
	}

	// reporting of size for directories is inconsistent
	if !a.IsDir() {
		if a.Size() != b.Size() {
			return fmt.Errorf("Size(): %d != %d", a.Size(), b.Size())
		}
	}

	if a.Mode() != b.Mode() {
		return fmt.Errorf("Mode(): %s != %s", a.Mode(), b.Mode())
	}

	if !a.ModTime().Truncate(time.Minute).Equal(b.ModTime().Truncate(time.Minute)) {
		return fmt.Errorf("ModTime() %s != %s", a.ModTime(), b.ModTime())
	}

	if a.IsDir() != b.IsDir() {
		return fmt.Errorf("IsDir(): %v != %v", a.IsDir(), b.IsDir())
	}

	return nil
}

func TestReadDir(t *testing.T) {
	for _, addr := range ftpdAddrs {
		c, err := DialConfig(goftpConfig, addr)

		if err != nil {
			t.Fatal(err)
		}

		list, err := c.ReadDir("")

		if err != nil {
			t.Fatal(err)
		}

		if len(list) != 3 {
			t.Errorf("expected 3 items, got %d", len(list))
		}

		var names []string

		for _, item := range list {
			expected, err := os.Stat("testroot/" + item.Name())
			if err != nil {
				t.Fatal(err)
			}

			if err := compareFileInfos(item, expected); err != nil {
				t.Errorf("mismatch on %s: %s (%s)", item.Name(), err, item.Sys().(string))
			}

			names = append(names, item.Name())
		}

		// sanity check names are what we expected
		sort.Strings(names)
		if !reflect.DeepEqual(names, []string{"git-ignored", "lorem.txt", "subdir"}) {
			t.Errorf("got: %v", names)
		}

		if c.numOpenConns() != len(c.freeConnCh) {
			t.Error("Leaked a connection")
		}
	}
}

func TestReadDirNoMLSD(t *testing.T) {
	// pureFTPD seems to have some issues with timestamps in LIST output
	for _, addr := range proAddrs {
		config := goftpConfig
		config.stubResponses = map[string]stubResponse{
			"MLSD ": {500, "'MLSD ': command not understood."},
		}

		c, err := DialConfig(config, addr)

		if err != nil {
			t.Fatal(err)
		}

		list, err := c.ReadDir("")

		if err != nil {
			t.Fatal(err)
		}

		if len(list) != 3 {
			t.Errorf("expected 3 items, got %d", len(list))
		}

		var names []string

		for _, item := range list {
			expected, err := os.Stat("testroot/" + item.Name())
			if err != nil {
				t.Fatal(err)
			}

			if err := compareFileInfos(item, expected); err != nil {
				t.Errorf("mismatch on %s: %s (%s)", item.Name(), err, item.Sys().(string))
			}

			names = append(names, item.Name())
		}

		// sanity check names are what we expected
		sort.Strings(names)
		if !reflect.DeepEqual(names, []string{"git-ignored", "lorem.txt", "subdir"}) {
			t.Errorf("got: %v", names)
		}

		if c.numOpenConns() != len(c.freeConnCh) {
			t.Error("Leaked a connection")
		}
	}
}

func TestStat(t *testing.T) {
	for _, addr := range ftpdAddrs {
		c, err := DialConfig(goftpConfig, addr)

		if err != nil {
			t.Fatal(err)
		}

		// check root
		info, err := c.Stat("")
		if err != nil {
			t.Fatal(err)
		}

		// work around inconsistency between pure-ftpd and proftpd
		var realStat os.FileInfo
		if info.Name() == "testroot" {
			realStat, err = os.Stat("testroot")
		} else {
			realStat, err = os.Stat("testroot/.")
		}
		if err != nil {
			t.Fatal(err)
		}

		if err := compareFileInfos(info, realStat); err != nil {
			t.Error(err)
		}

		// check a file
		info, err = c.Stat("subdir/1234.bin")
		if err != nil {
			t.Fatal(err)
		}

		realStat, err = os.Stat("testroot/subdir/1234.bin")
		if err != nil {
			t.Fatal(err)
		}

		if err := compareFileInfos(info, realStat); err != nil {
			t.Error(err)
		}

		// check a directory
		info, err = c.Stat("subdir")
		if err != nil {
			t.Fatal(err)
		}

		realStat, err = os.Stat("testroot/subdir")
		if err != nil {
			t.Fatal(err)
		}

		if err := compareFileInfos(info, realStat); err != nil {
			t.Error(err)
		}

		if c.numOpenConns() != len(c.freeConnCh) {
			t.Error("Leaked a connection")
		}
	}
}

func TestStatNoMLST(t *testing.T) {
	// pureFTPD seems to have some issues with timestamps in LIST output
	for _, addr := range proAddrs {
		config := goftpConfig
		config.stubResponses = map[string]stubResponse{
			"MLST ":                {500, "'MLST ': command not understood."},
			"MLST subdir/1234.bin": {500, "'MLST ': command not understood."},
			"MLST subdir":          {500, "'MLST ': command not understood."},
		}

		c, err := DialConfig(config, addr)

		if err != nil {
			t.Fatal(err)
		}

		// check a file
		info, err := c.Stat("subdir/1234.bin")
		if err != nil {
			t.Fatal(err)
		}

		realStat, err := os.Stat("testroot/subdir/1234.bin")
		if err != nil {
			t.Fatal(err)
		}

		if err := compareFileInfos(info, realStat); err != nil {
			t.Error(err)
		}

		if c.numOpenConns() != len(c.freeConnCh) {
			t.Error("Leaked a connection")
		}
	}
}
func TestGetwd(t *testing.T) {
	for _, addr := range ftpdAddrs {
		c, err := DialConfig(goftpConfig, addr)

		if err != nil {
			t.Fatal(err)
		}

		cwd, err := c.Getwd()
		if err != nil {
			t.Fatal(err)
		}

		realCwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}

		if cwd != "/" && cwd != path.Join(realCwd, "testroot") {
			t.Errorf("Unexpected cwd: %s", cwd)
		}

		// cd into quote directory so we can test Getwd's quote handling
		os.Remove(`testroot/git-ignored/dir-with-"`)
		dir, err := c.Mkdir(`git-ignored/dir-with-"`)
		if err != nil {
			t.Fatal(err)
		}

		pconn, err := c.getIdleConn()
		if err != nil {
			t.Fatal(err)
		}

		err = pconn.sendCommandExpected(replyFileActionOkay, "CWD %s", dir)
		c.returnConn(pconn)

		if err != nil {
			t.Fatal(err)
		}

		dir, err = c.Getwd()
		if dir != `git-ignored/dir-with-"` && dir != path.Join(cwd, `git-ignored/dir-with-"`) {
			t.Errorf("Unexpected dir-with-quote value: %s", dir)
		}

		if c.numOpenConns() != len(c.freeConnCh) {
			t.Error("Leaked a connection")
		}
	}
}
