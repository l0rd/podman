package compression

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/containers/podman/v5/pkg/machine/define"
	"github.com/containers/podman/v5/utils"
	"github.com/containers/storage/pkg/archive"
	"github.com/sirupsen/logrus"
)

const (
	decompressedFileFlag = os.O_CREATE | os.O_TRUNC | os.O_WRONLY
	macOs                = "darwin"
	progressBarPrefix    = "Extracting compressed file"
	zipExt               = ".zip"
)

type decompressor interface {
	compressedFileSize() int64
	compressedFileMode() os.FileMode
	compressedFileReader() (io.ReadCloser, error)
	decompress(w WriteSeekCloser, r io.Reader) error
	decompressSparse(w WriteSeekCloser, r io.Reader) error
	close()
}

func Decompress(compressedVMFile *define.VMFile, decompressedFilePath string) error {
	compressedFilePath := compressedVMFile.GetPath()
	// Are we reading full image file?
	// Only few bytes are read to detect
	// the compression type
	compressedFileContent, err := compressedVMFile.Read()
	if err != nil {
		return err
	}

	var d decompressor
	if d, err = newDecompressor(compressedFilePath, compressedFileContent); err != nil {
		return err
	}

	return runDecompression(d, decompressedFilePath, false)
}

func DecompressSparse(compressedVMFile *define.VMFile, decompressedFilePath string) error {
	compressedFilePath := compressedVMFile.GetPath()
	// Are we reading full image file?
	// Only few bytes are read to detect
	// the compression type
	compressedFileContent, err := compressedVMFile.Read()
	if err != nil {
		return err
	}

	var d decompressor
	if d, err = newDecompressor(compressedFilePath, compressedFileContent); err != nil {
		return err
	}

	return runDecompression(d, decompressedFilePath, true)
}

func newDecompressor(compressedFilePath string, compressedFileContent []byte) (decompressor, error) {
	compressionType := archive.DetectCompression(compressedFileContent)
	os := runtime.GOOS
	hasZipSuffix := strings.HasSuffix(compressedFilePath, zipExt)

	switch {
	case compressionType == archive.Xz:
		return newXzDecompressor(compressedFilePath)
	// Zip files are not guaranteed to have a magic number at the beginning
	// of the file, so we need to use the file name to detect them.
	case compressionType == archive.Uncompressed && hasZipSuffix:
		return newZipDecompressor(compressedFilePath)
	case compressionType == archive.Uncompressed:
		return newUncompressedDecompressor(compressedFilePath)
	// Using special compressors on MacOS because default ones
	// in c/image/pkg/compression are slow with sparse files.
	case compressionType == archive.Gzip && os == macOs:
		return newGzipDecompressor(compressedFilePath)
	case compressionType == archive.Zstd && os == macOs:
		return newZstdDecompressor(compressedFilePath)
	default:
		return newGenericDecompressor(compressedFilePath)
	}
}

func runDecompression(d decompressor, decompressedFilePath string, sparse bool) error {
	compressedFileReader, err := d.compressedFileReader()
	if err != nil {
		return err
	}
	defer d.close()

	initMsg := progressBarPrefix + ": " + filepath.Base(decompressedFilePath)
	finalMsg := initMsg + ": done"

	p, bar := utils.ProgressBar(initMsg, d.compressedFileSize(), finalMsg)
	// Wait for bars to complete and then shut down the bars container
	defer p.Wait()

	compressedFileReaderProxy := bar.ProxyReader(compressedFileReader)
	// Interrupts the bar goroutine. It's important that
	// bar.Abort(false) is called before p.Wait(), otherwise
	// can hang.
	defer bar.Abort(false)

	var decompressedFileWriter *os.File

	if decompressedFileWriter, err = os.OpenFile(decompressedFilePath, decompressedFileFlag, d.compressedFileMode()); err != nil {
		logrus.Errorf("Unable to open destination file %s for writing: %q", decompressedFilePath, err)
		return err
	}
	defer func() {
		if err := decompressedFileWriter.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			logrus.Warnf("Unable to to close destination file %s: %q", decompressedFilePath, err)
		}
	}()

	if sparse {
		if err = d.decompressSparse(decompressedFileWriter, compressedFileReaderProxy); err != nil {
			logrus.Errorf("Error extracting compressed file in sparse mode: %q", err)
			return err
		}
	} else {
		if err = d.decompress(decompressedFileWriter, compressedFileReaderProxy); err != nil {
			logrus.Errorf("Error extracting compressed file: %q", err)
			return err
		}
	}

	return nil
}
