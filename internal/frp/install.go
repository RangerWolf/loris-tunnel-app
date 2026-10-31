package frp

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"loris-tunnel/internal/climaint"
	"path/filepath"
)

func (c *Client) Install(ctx context.Context) error {
	manager := climaint.New(c.MaintenanceSpec())
	status, err := manager.Check(ctx, true)
	if err != nil {
		return err
	}
	_, err = manager.Apply(ctx, status.CandidateID, func() (func(), error) { return func() {}, nil })
	return err
}

func extractBinary(data []byte, zipped bool) ([]byte, error) {
	const max = 80 << 20
	if zipped {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, file := range reader.File {
			if filepath.Base(file.Name) == executableName() {
				r, err := file.Open()
				if err != nil {
					return nil, err
				}
				defer r.Close()
				b, err := io.ReadAll(io.LimitReader(r, max+1))
				if len(b) > max {
					return nil, fmt.Errorf("FRP executable too large")
				}
				return b, err
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader := tar.NewReader(gz)
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == executableName() {
				b, err := io.ReadAll(io.LimitReader(reader, max+1))
				if len(b) > max {
					return nil, fmt.Errorf("FRP executable too large")
				}
				return b, err
			}
		}
	}
	return nil, fmt.Errorf("official archive does not contain frpc")
}
