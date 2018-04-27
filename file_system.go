// Copyright 2015 Muir Manders.  All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package goftp

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// time.Parse format string for parsing file mtimes.
const timeFormat = "20060102150405"

// Delete deletes the file "path".
func (c *Client) Delete(path string) error {
	pconn, err := c.getIdleConn()
	if err != nil {
		return err
	}

	defer c.returnConn(pconn)

	return pconn.sendCommandExpected(replyFileActionOkay, "DELE %s", path)
}

// Rename renames file "from" to "to".
func (c *Client) Rename(from, to string) error {
	pconn, err := c.getIdleConn()
	if err != nil {
		return err
	}

	defer c.returnConn(pconn)

	err = pconn.sendCommandExpected(replyFileActionPending, "RNFR %s", from)
	if err != nil {
		return err
	}

	return pconn.sendCommandExpected(replyFileActionOkay, "RNTO %s", to)
}

// Mkdir creates directory "path". The returned string is how the client
// should refer to the created directory.
func (c *Client) Mkdir(path string) (string, error) {
	pconn, err := c.getIdleConn()
	if err != nil {
		return "", err
	}

	defer c.returnConn(pconn)

	code, msg, err := pconn.sendCommand("MKD %s", path)
	if err != nil {
		return "", err
	}

	if code != replyDirCreated {
		return "", ftpError{code: code, msg: msg}
	}

	dir, err := extractDirName(msg)
	if err != nil {
		return "", err
	}

	return dir, nil
}

// Rmdir removes directory "path".
func (c *Client) Rmdir(path string) error {
	pconn, err := c.getIdleConn()
	if err != nil {
		return err
	}

	defer c.returnConn(pconn)

	return pconn.sendCommandExpected(replyFileActionOkay, "RMD %s", path)
}

// Getwd returns the current working directory.
func (c *Client) Getwd() (string, error) {
	pconn, err := c.getIdleConn()
	if err != nil {
		return "", err
	}

	defer c.returnConn(pconn)

	code, msg, err := pconn.sendCommand("PWD")
	if err != nil {
		return "", err
	}

	if code != replyDirCreated {
		return "", ftpError{code: code, msg: msg}
	}

	dir, err := extractDirName(msg)
	if err != nil {
		return "", err
	}

	return dir, nil
}

func commandNotSupporterdError(err error) bool {
	respCode := err.(ftpError).Code()
	return respCode == replyCommandSyntaxError || respCode == replyCommandNotImplemented
}

// ReadDir fetches the contents of a directory, returning a list of
// os.FileInfo's which are relatively easy to work with programatically. It
// will not return entries corresponding to the current directory or parent
// directories. The os.FileInfo's fields may be incomplete depending on what
// the server supports. If the server does not support "MLSD", "LIST" will
// be used. You may have to set ServerLocation in your config to get (more)
// accurate ModTimes in this case.
func (c *Client) ReadDir(path string) ([]os.FileInfo, error) {
	entries, err := c.dataStringList("MLSD %s", path)

	parser := parseMLST

	if err != nil {
		if !commandNotSupporterdError(err) {
			return nil, err
		}

		entries, err = c.dataStringList("LIST %s", path)
		if err != nil {
			return nil, err
		}
		parser = func(entry string, skipSelfParent bool) (os.FileInfo, error) {
			return parseLIST(entry, c.config.ServerLocation, skipSelfParent)
		}
	}

	var ret []os.FileInfo
	for _, entry := range entries {
		info, err := parser(entry, true)
		if err != nil {
			c.debug("error in ReadDir: %s", err)
			return nil, err
		}

		if info == nil {
			continue
		}

		ret = append(ret, info)
	}

	return ret, nil
}

// Stat fetches details for a particular file. The os.FileInfo's fields may
// be incomplete depending on what the server supports. If the server doesn't
// support "MLST", "LIST" will be attempted, but "LIST" will not work if path
// is a directory. You may have to set ServerLocation in your config to get
// (more) accurate ModTimes when using "LIST".
func (c *Client) Stat(path string) (os.FileInfo, error) {
	lines, err := c.controlStringList("MLST %s", path)
	if err != nil {
		if commandNotSupporterdError(err) {
			lines, err = c.dataStringList("LIST %s", path)
			if err != nil {
				return nil, err
			}

			if len(lines) != 1 {
				return nil, ftpError{err: fmt.Errorf("unexpected LIST response: %v", lines)}
			}

			return parseLIST(lines[0], c.config.ServerLocation, false)
		}
		return nil, err
	}

	if len(lines) != 3 {
		return nil, ftpError{err: fmt.Errorf("unexpected MLST response: %v", lines)}
	}

	return parseMLST(strings.TrimLeft(lines[1], " "), false)
}

func extractDirName(msg string) (string, error) {
	openQuote := strings.Index(msg, "\"")
	closeQuote := strings.LastIndex(msg, "\"")
	if openQuote == -1 || len(msg) == openQuote+1 || closeQuote <= openQuote {
		return "", ftpError{
			err: fmt.Errorf("failed parsing directory name: %s", msg),
		}
	}
	return strings.Replace(msg[openQuote+1:closeQuote], `""`, `"`, -1), nil
}

func (c *Client) controlStringList(f string, args ...interface{}) ([]string, error) {
	pconn, err := c.getIdleConn()
	if err != nil {
		return nil, err
	}

	defer c.returnConn(pconn)

	cmd := fmt.Sprintf(f, args...)

	code, msg, err := pconn.sendCommand(cmd)

	if !positiveCompletionReply(code) {
		pconn.debug("unexpected response to %s: %d-%s", cmd, code, msg)
		return nil, ftpError{code: code, msg: msg}
	}

	return strings.Split(msg, "\n"), nil
}

func (c *Client) dataStringList(f string, args ...interface{}) ([]string, error) {
	pconn, err := c.getIdleConn()
	if err != nil {
		return nil, err
	}

	defer c.returnConn(pconn)

	dcGetter, err := pconn.prepareDataConn()
	if err != nil {
		return nil, err
	}

	cmd := fmt.Sprintf(f, args...)

	err = pconn.sendCommandExpected(replyGroupPreliminaryReply, cmd)
	if err != nil {
		return nil, err
	}

	dc, err := dcGetter()
	if err != nil {
		return nil, err
	}

	// to catch early returns
	defer dc.Close()

	scanner := bufio.NewScanner(dc)
	scanner.Split(bufio.ScanLines)

	var res []string
	for scanner.Scan() {
		res = append(res, scanner.Text())
	}

	var dataError error
	if err = scanner.Err(); err != nil {
		pconn.debug("error reading %s data: %s", cmd, err)
		dataError = ftpError{
			err:       fmt.Errorf("error reading %s data: %s", cmd, err),
			temporary: true,
		}
	}

	err = dc.Close()
	if err != nil {
		pconn.debug("error closing data connection: %s", err)
	}

	code, msg, err := pconn.readResponse()
	if err != nil {
		return nil, err
	}

	if !positiveCompletionReply(code) {
		pconn.debug("unexpected result: %d-%s", code, msg)
		return nil, ftpError{code: code, msg: msg}
	}

	if dataError != nil {
		return nil, dataError
	}

	return res, nil
}

type ftpFile struct {
	name  string
	size  int64
	mode  os.FileMode
	mtime time.Time
	raw   string
}

func (f *ftpFile) Name() string {
	return f.name
}

func (f *ftpFile) Size() int64 {
	return f.size
}

func (f *ftpFile) Mode() os.FileMode {
	return f.mode
}

func (f *ftpFile) ModTime() time.Time {
	return f.mtime
}

func (f *ftpFile) IsDir() bool {
	return f.mode.IsDir()
}

func (f *ftpFile) Sys() interface{} {
	return f.raw
}

var lsRegex = regexp.MustCompile(`^\s*(\S)(\S{3})(\S{3})(\S{3})(?:\s+\S+){3}\s+(\d+)\s+(\w+\s+\d+)\s+([\d:]+)\s+(.+)$`)

// total 404456
// drwxr-xr-x   8 goftp    20            272 Jul 28 05:03 git-ignored
func parseLIST(entry string, loc *time.Location, skipSelfParent bool) (os.FileInfo, error) {
	if strings.HasPrefix(entry, "total ") {
		return nil, nil
	}

	matches := lsRegex.FindStringSubmatch(entry)
	if len(matches) == 0 {
		return nil, ftpError{err: fmt.Errorf(`failed parsing LIST entry: %s`, entry)}
	}

	if skipSelfParent && (matches[8] == "." || matches[8] == "..") {
		return nil, nil
	}

	var mode os.FileMode
	switch matches[1] {
	case "d":
		mode |= os.ModeDir
	case "l":
		mode |= os.ModeSymlink
	}

	for i := 0; i < 3; i++ {
		if matches[i+2][0] == 'r' {
			mode |= os.FileMode(04 << (3 * uint(2-i)))
		}
		if matches[i+2][1] == 'w' {
			mode |= os.FileMode(02 << (3 * uint(2-i)))
		}
		if matches[i+2][2] == 'x' || matches[i+2][2] == 's' {
			mode |= os.FileMode(01 << (3 * uint(2-i)))
		}
	}

	size, err := strconv.ParseUint(matches[5], 10, 64)
	if err != nil {
		return nil, ftpError{err: fmt.Errorf(`failed parsing LIST entry's size: %s (%s)`, err, entry)}
	}

	var mtime time.Time
	if strings.Contains(matches[7], ":") {
		mtime, err = time.ParseInLocation("Jan _2 15:04", matches[6]+" "+matches[7], loc)
		if err == nil {
			now := time.Now()
			year := now.Year()
			if mtime.Month() > now.Month() {
				year--
			}
			mtime, err = time.ParseInLocation("Jan _2 15:04 2006", matches[6]+" "+matches[7]+" "+strconv.Itoa(year), loc)
		}
	} else {
		mtime, err = time.ParseInLocation("Jan _2 2006", matches[6]+" "+matches[7], loc)
	}

	if err != nil {
		return nil, ftpError{err: fmt.Errorf(`failed parsing LIST entry's mtime: %s (%s)`, err, entry)}
	}

	info := &ftpFile{
		name:  filepath.Base(matches[8]),
		mode:  mode,
		mtime: mtime,
		raw:   entry,
		size:  int64(size),
	}

	return info, nil
}

type mlstParser struct{}

func parseMLST(entry string, skipSelfParent bool) (os.FileInfo, error) {
	return mlstParser{}.parse(entry, skipSelfParent)
}

type mlstToken int

const (
	mlstFactName mlstToken = iota
	mlstFactValue
	mlstFilename
)

type mlstFacts struct {
	typ      string
	unixMode string
	perm     string
	size     string
	sizd     string
	modify   string
}

// an entry looks something like this:
// type=file;size=12;modify=20150216084148;UNIX.mode=0644;unique=1000004g1187ec7; lorem.txt
func (p mlstParser) parse(entry string, skipSelfParent bool) (os.FileInfo, error) {
	var facts mlstFacts
	state := mlstFactName
	var left string // Previous token.
	var i1 int      // Current token's start position.
	for i2, r := range entry {
		switch r {
		case ';':
			if state == mlstFactValue {
				if left == "" {
					return nil, p.error(entry)
				}
				var (
					key = strings.ToLower(left[:len(left)-1])
					val = strings.ToLower(entry[i1:i2])
				)
				switch key {
				case "type":
					facts.typ = val
				case "unix.mode":
					facts.unixMode = val
				case "perm":
					facts.perm = val
				case "size":
					facts.size = val
				case "sizd":
					facts.sizd = val
				case "modify":
					facts.modify = val
				}
				if len(entry) >= i2+1 && entry[i2+1] == ' ' {
					state = mlstFilename
				} else {
					state = mlstFactName
				}
				i1 = i2 + 1
			}
		case '=':
			switch state {
			case mlstFactName:
				left = entry[i1 : i2+1]
				i1 = i2 + 1
				state = mlstFactValue
			}
		}
	}
	if state != mlstFilename || i1+1 >= len(entry) {
		return nil, p.error(entry)
	}
	filename := entry[i1+1:]

	typ := facts.typ

	if typ == "" {
		return nil, p.incompleteError(entry)
	}

	if skipSelfParent && (typ == "cdir" || typ == "pdir" || typ == "." || typ == "..") {
		return nil, nil
	}

	var mode os.FileMode
	if facts.unixMode != "" {
		m, err := strconv.ParseInt(facts.unixMode, 8, 32)
		if err != nil {
			return nil, p.error(entry)
		}
		mode = os.FileMode(m)
	} else if facts.perm != "" {
		// see http://tools.ietf.org/html/rfc3659#section-7.5.5
		for _, c := range facts.perm {
			switch c {
			case 'a', 'd', 'c', 'f', 'm', 'p', 'w':
				// these suggest you have write permissions
				mode |= 0200
			case 'l':
				// can list dir entries means readable and executable
				mode |= 0500
			case 'r':
				// readable file
				mode |= 0400
			}
		}
	} else {
		// no mode info, just say it's readable to us
		mode = 0400
	}

	if typ == "dir" || typ == "cdir" || typ == "pdir" {
		mode |= os.ModeDir
	} else if strings.HasPrefix(typ, "os.unix=slink") || strings.HasPrefix(typ, "os.unix=symlink") {
		// note: there is no general way to determine whether a symlink points to a dir or a file
		mode |= os.ModeSymlink
	}

	var (
		size int64
		err  error
	)

	if facts.size != "" {
		size, err = strconv.ParseInt(facts.size, 10, 64)
	} else if mode.IsDir() && facts.sizd != "" {
		size, err = strconv.ParseInt(facts.sizd, 10, 64)
	} else if typ == "file" {
		return nil, p.incompleteError(entry)
	}

	if err != nil {
		return nil, p.error(entry)
	}

	if facts.modify == "" {
		return nil, p.incompleteError(entry)
	}

	mtime, ok := p.parseModTime(facts.modify)
	if !ok {
		return nil, p.incompleteError(entry)
	}

	info := &ftpFile{
		name:  filepath.Base(filename),
		size:  size,
		mtime: mtime,
		raw:   entry,
		mode:  mode,
	}

	return info, nil
}

func (p mlstParser) error(entry string) error {
	return ftpError{err: fmt.Errorf(`failed parsing MLST entry: %s`, entry)}
}

func (p mlstParser) incompleteError(entry string) error {
	return ftpError{err: fmt.Errorf(`MLST entry incomplete: %s`, entry)}
}

// parseModTime parses file mtimes formatted as 20060102150405.
func (p *mlstParser) parseModTime(value string) (time.Time, bool) {
	if len(value) != 14 {
		return time.Time{}, false
	}
	year, err := strconv.ParseUint(value[:4], 10, 16)
	if err != nil {
		return time.Time{}, false
	}
	month, err := strconv.ParseUint(value[4:6], 10, 8)
	if err != nil {
		return time.Time{}, false
	}
	day, err := strconv.ParseUint(value[6:8], 10, 8)
	if err != nil {
		return time.Time{}, false
	}
	hour, err := strconv.ParseUint(value[8:10], 10, 8)
	if err != nil {
		return time.Time{}, false
	}
	min, err := strconv.ParseUint(value[10:12], 10, 8)
	if err != nil {
		return time.Time{}, false
	}
	sec, err := strconv.ParseUint(value[12:14], 10, 8)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(int(year), time.Month(month), int(day),
		int(hour), int(min), int(sec), 0, time.UTC), true
}
