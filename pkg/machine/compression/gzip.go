package compression

import (
	"io"

	image "github.com/containers/image/v5/pkg/compression"
	"github.com/sirupsen/logrus"
)

type gzipDecompressor struct {
	genericDecompressor
}

func newGzipDecompressor(compressedFilePath string) (*gzipDecompressor, error) {
	d, err := newGenericDecompressor(compressedFilePath)
	return &gzipDecompressor{*d}, err
}

func (d *gzipDecompressor) decompress(w WriteSeekCloser, r io.Reader) error {
	gzReader, err := image.GzipDecompressor(r)
	if err != nil {
		return err
	}
	defer func() {
		if err := gzReader.Close(); err != nil {
			logrus.Errorf("Unable to close gz file: %q", err)
		}
	}()

	_, err = io.Copy(w, gzReader)
	return err
}

func (d *gzipDecompressor) decompressSparse(w WriteSeekCloser, r io.Reader) error {
	gzReader, err := image.GzipDecompressor(r)
	if err != nil {
		return err
	}
	defer func() {
		if err := gzReader.Close(); err != nil {
			logrus.Errorf("Unable to close gz file: %q", err)
		}
	}()

	sparseWriter := NewSparseWriter(w)
	defer func() {
		if err := sparseWriter.Close(); err != nil {
			logrus.Errorf("Unable to close uncompressed file: %q", err)
		}
	}()

	_, err = io.Copy(sparseWriter, gzReader)
	return err
}
