package utils

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/josephspurrier/goversioninfo"
)

//go:embed resources/*
var embedFS embed.FS

// WindowsResource create a Windows resource system object
// for embedding into the Caddy binary.
// reference: https://github.com/rclone/rclone/blob/v1.66.0/bin/resource_windows.go
func WindowsResource(version, outputFile, tempDir string) error {
	vi := &goversioninfo.VersionInfo{}

	// FixedFileInfo
	vi.FixedFileInfo.FileOS = "040004" // VOS_NT_WINDOWS32
	vi.FixedFileInfo.FileType = "01"   // VFT_APP

	semanticVersion, err := semver.NewVersion(version)
	if err != nil {
		return err
	}

	basename := filepath.Base(outputFile)
	ext := filepath.Ext(basename)

	// FixedFileInfo.FileVersion
	vi.FixedFileInfo.FileVersion.Major = int(semanticVersion.Major())
	vi.FixedFileInfo.FileVersion.Minor = int(semanticVersion.Minor())
	vi.FixedFileInfo.FileVersion.Patch = int(semanticVersion.Patch())
	vi.FixedFileInfo.FileVersion.Build = 0
	// FixedFileInfo.ProductVersion
	vi.FixedFileInfo.ProductVersion.Major = int(semanticVersion.Major())
	vi.FixedFileInfo.ProductVersion.Minor = int(semanticVersion.Minor())
	vi.FixedFileInfo.ProductVersion.Patch = int(semanticVersion.Patch())
	vi.FixedFileInfo.ProductVersion.Build = 0

	// StringFileInfo
	vi.StringFileInfo.CompanyName = "https://caddyserver.com/"
	vi.StringFileInfo.ProductName = "Caddy"
	vi.StringFileInfo.FileDescription = "Caddy"
	vi.StringFileInfo.InternalName = strings.TrimSuffix(basename, ext)
	vi.StringFileInfo.OriginalFilename = basename
	vi.StringFileInfo.LegalCopyright = "The Caddy Authors"
	vi.StringFileInfo.FileVersion = semanticVersion.String()
	vi.StringFileInfo.ProductVersion = semanticVersion.String()

	// extract ico file from embed to an actual file
	ico, err := embedFS.ReadFile("resources/ico/caddy.ico")
	if err != nil {
		return err
	}
	icoCopy, err := os.Create(filepath.Join(tempDir, "caddy.ico"))
	if err != nil {
		return err
	}
	// set ico path
	vi.IconPath = icoCopy.Name()
	_, err = icoCopy.Write(ico)
	if err != nil {
		return err
	}
	err = icoCopy.Close()
	if err != nil {
		return err
	}

	// Build native structures from the configuration data
	vi.Build()

	// Write the native structures as binary data to a buffer
	vi.Walk()

	arch := GetGOARCH()

	// Write the binary data buffer to file
	return vi.WriteSyso(filepath.Join(tempDir, fmt.Sprintf("resource_windows_%s.syso", arch)), arch)
}
