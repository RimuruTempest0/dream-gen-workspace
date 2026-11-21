//go:build desktopqa

package main

// SeedTestImageSuite saves a suite with a sample, two versions, an uploaded
// product photo and a crop (nothing generated) and returns its run ID (#19).
func (a *App) SeedTestImageSuite() (string, error) { return a.service.SeedTestImageSuite() }
