package compression

import (
	"io"
	"io/fs"
	"os"

	"github.com/containers/image/v5/pkg/compression"
	"github.com/sirupsen/logrus"
)

type genericDecompressor struct {
	compressedFilePath string
	compressedFile     *os.File
	compressedFileInfo os.FileInfo
}

func newGenericDecompressor(compressedFilePath string) (*genericDecompressor, error) {
	d := &genericDecompressor{}
	d.compressedFilePath = compressedFilePath
	stat, err := os.Stat(d.compressedFilePath)
	if err != nil {
		return nil, err
	}
	d.compressedFileInfo = stat
	return d, nil
}

func (d *genericDecompressor) compressedFileSize() int64 {
	return d.compressedFileInfo.Size()
}

func (d *genericDecompressor) compressedFileMode() fs.FileMode {
	return d.compressedFileInfo.Mode()
}

func (d *genericDecompressor) compressedFileReader() (io.ReadCloser, error) {
	compressedFile, err := os.Open(d.compressedFilePath)
	if err != nil {
		return nil, err
	}
	d.compressedFile = compressedFile
	return compressedFile, nil
}

func (d *genericDecompressor) decompress(w WriteSeekCloser, r io.Reader) error {
	decompressedFileReader, _, err := compression.AutoDecompress(r)
	if err != nil {
		return err
	}
	defer func() {
		if err := decompressedFileReader.Close(); err != nil {
			logrus.Errorf("Unable to close gz file: %q", err)
		}
	}()

	_, err = io.Copy(w, decompressedFileReader)
	return err
}

func (d *genericDecompressor) decompressSparse(w WriteSeekCloser, r io.Reader) error {
	decompressedFileReader, _, err := compression.AutoDecompress(r)
	if err != nil {
		return err
	}
	defer func() {
		if err := decompressedFileReader.Close(); err != nil {
			logrus.Errorf("Unable to close gz file: %q", err)
		}
	}()

	sparseWriter := NewSparseWriter(w)
	defer func() {
		if err := sparseWriter.Close(); err != nil {
			logrus.Errorf("Unable to close uncompressed file: %q", err)
		}
	}()
	
	_, err = io.Copy(sparseWriter, decompressedFileReader)
	return err
}

func (d *genericDecompressor) close() {
	if err := d.compressedFile.Close(); err != nil {
		logrus.Errorf("Unable to close compressed file: %q", err)
	}
}
