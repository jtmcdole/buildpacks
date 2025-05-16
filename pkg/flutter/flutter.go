// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package flutter provides utility methods for building Flutter Dart applications.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/GoogleCloudPlatform/buildpacks/pkg/buildererror"
	"github.com/GoogleCloudPlatform/buildpacks/pkg/env"
	gcp "github.com/GoogleCloudPlatform/buildpacks/pkg/gcpbuildpack"
	"github.com/hashicorp/go-retryablehttp"
	"gopkg.in/yaml.v2"
)

var versionURL = "https://storage.googleapis.com/flutter_infra_release/releases/releases_linux.json"

// releaseDetail contains information about specific releases
type releaseDetail struct {
	Hash           string    `json:"hash"`
	Channel        string    `json:"channel"`
	Version        string    `json:"version"`
	DartSdkVersion string    `json:"dart_sdk_version,omitempty"`
	DartSdkArch    string    `json:"dart_sdk_arch,omitempty"`
	ReleaseDate    time.Time `json:"release_date"`
	Archive        string    `json:"archive"`
	Sha256         string    `json:"sha256"`
}

// releaseInfo contains information about a the current releases
type releaseInfo struct {
	BaseURL        string `json:"base_url"`
	CurrentRelease struct {
		Beta   string `json:"beta"`
		Dev    string `json:"dev"`
		Stable string `json:"stable"`
	} `json:"current_release"`
	Releases []releaseDetail `json:"releases"`
}

// Pubspec represents a small view of a pubspec.yaml.
type Pubspec struct {
	Dependencies    map[string]interface{} `yaml:"dependencies"`
	DevDependencies map[string]interface{} `yaml:"dev_dependencies"`
	Buildpack       struct {
		Server string `default:"server" yaml:"server"`
		Static string `default:"app" yaml:"static"`
	} `yaml:"buildpack"`
}

// findStableRelease searches for the stable release based on CurrentRelease.Stable hash.
func findStableRelease(info releaseInfo) (releaseDetail, bool) {
	stableHash := info.CurrentRelease.Stable
	if stableHash == "" {
		return releaseDetail{}, false
	}

	for _, release := range info.Releases {
		if release.Hash == stableHash {
			return release, true
		}
	}
	return releaseDetail{}, false
}

func findSpecificRelease(version string, info releaseInfo) (releaseDetail, bool) {
	for _, release := range info.Releases {
		if release.Version == version {
			return release, true
		}
	}
	return releaseDetail{}, false
}

// DetectSDKVersion detects which SDK version should be installed from the environment or fetches
// the latest stable available version. Returns version, url
func DetectSDKArchive() (string, string, error) {
	if envVersion := os.Getenv(env.RuntimeVersion); envVersion != "" {
		detail, err := fetchSpecificSdkArchive(envVersion)
		if err != nil {
			return "", "", err
		}
		return detail.Version, detail.Archive, nil
	}
	detail, err := fetchLatestSdkArchive()
	if err != nil {
		return "", "", err
	}
	return detail.Version, detail.Archive, nil
}

func downloadManifest() (releaseInfo, error) {
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 3

	resp, err := retryClient.StandardClient().Get(versionURL)
	if err != nil {
		return releaseInfo{}, buildererror.InternalErrorf("fetching Dart SDK version from %q: %v", versionURL, err)
	}
	defer resp.Body.Close()

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return releaseInfo{}, buildererror.InternalErrorf("reading response: %v", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return releaseInfo{}, buildererror.InternalErrorf("unexpected status code from %q: %d (%s)", versionURL, resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	var info releaseInfo
	if err := json.Unmarshal(bytes, &info); err != nil {
		return releaseInfo{}, buildererror.InternalErrorf("unmarshalling response from %q: %v", versionURL, err)
	}
	return info, nil
}

func fetchSpecificSdkArchive(version string) (releaseDetail, error) {
	info, err := downloadManifest()
	if err != nil {
		return releaseDetail{}, err
	}

	detail, found := findSpecificRelease(version, info)
	if !found {
		return releaseDetail{}, buildererror.InternalErrorf("version not found %q", version)
	}
	return detail, nil
}

func fetchLatestSdkArchive() (releaseDetail, error) {
	info, err := downloadManifest()
	if err != nil {
		return releaseDetail{}, err
	}

	detail, found := findStableRelease(info)
	if !found {
		return releaseDetail{}, buildererror.InternalErrorf("stablke version not found")
	}
	return detail, nil
}

// IsFlutter returns true if the given Dart project contains a pubspec.yaml that declares a
// dependency on flutter.
func IsFlutter(dir string) (bool, error) {
	f := filepath.Join(dir, "pubspec.yaml")
	rawpjs, err := ioutil.ReadFile(f)
	if os.IsNotExist(err) {
		// If there is no pubspec.yaml, there is no build_runner dependency.
		return false, nil
	}
	if err != nil {
		return false, gcp.InternalErrorf("reading pubspec.yaml: %v", err)
	}

	var ps Pubspec
	if err := yaml.Unmarshal(rawpjs, &ps); err != nil {
		return false, gcp.UserErrorf("unmarshalling pubspec.yaml: %v", err)
	}

	if _, exists := ps.Dependencies["flutter"]; exists {
		return true, nil
	}
	return false, nil
}

func GetPubspec(dir string) (Pubspec, error) {
	f := filepath.Join(dir, "pubspec.yaml")
	rawpjs, err := ioutil.ReadFile(f)
	if os.IsNotExist(err) {
		// If there is no pubspec.yaml, there is no build_runner dependency.
		return Pubspec{}, nil
	}
	if err != nil {
		return Pubspec{}, gcp.InternalErrorf("reading pubspec.yaml: %v", err)
	}

	var ps Pubspec
	if err := yaml.Unmarshal(rawpjs, &ps); err != nil {
		return Pubspec{}, gcp.UserErrorf("unmarshalling pubspec.yaml: %v", err)
	}
	return ps, nil
}

func main() {
	var info Pubspec
	yamlString := `name: ffbah_example

workspace:
  - server
  - app

environment:
  sdk: ">=3.7.0 <4.0.0"

dependencies:
  flutter:
    sdk: flutter
  http: ^1.4.0
  shelf: ^1.4.0
  shelf_router: ^1.1.0

dev_dependencies:
  melos: ^7.0.0-dev.7
`
	if err := yaml.Unmarshal([]byte(yamlString), &info); err != nil {
		return
	}
	fmt.Printf("sup: %s", info)
	fmt.Printf("sup: %s", info.Buildpack.Server)
	fmt.Printf("sup: %s", info.Buildpack.Static)

}
