// +build !solution

package tarstream

import (
	"archive/tar"
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func Send(dir string, w io.Writer) error {
	err := os.Chdir(dir)
	check(err)

	tw := tar.NewWriter(w)

	err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		check(err)
		header, err := tar.FileInfoHeader(info, "")

		header.Name = path
		check(err)
		err = tw.WriteHeader(header)
		check(err)
		if !info.IsDir() {
			content, err := ioutil.ReadFile(path)
			check(err)
			_, err = tw.Write(content)
			check(err)
		}
		return nil
	})
	check(err)
	return nil
}

func Receive(dir string, r io.Reader) error {
	err := os.Chdir(dir)
	check(err)

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		check(err)
		info := hdr.FileInfo()
		if !info.IsDir() {
			buf := new(bytes.Buffer)

			_, _ = buf.ReadFrom(tr)

			_ = ioutil.WriteFile(filepath.Join(dir, hdr.Name), buf.Bytes(), info.Mode())
		} else {
			_ = os.Mkdir(hdr.Name, info.Mode())
		}
	}
	return nil
}
