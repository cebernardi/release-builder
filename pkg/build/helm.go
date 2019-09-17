package build

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/howardjohn/istio-release/pkg/model"
	"github.com/howardjohn/istio-release/pkg/util"

	"istio.io/pkg/log"
)

// Helm outputs the helm charts for the installation
func Helm(manifest model.Manifest) error {
	// Setup a working directory for helm
	helm := path.Join(manifest.WorkDir(), "helm")
	if err := os.MkdirAll(path.Join(helm, "packages"), 0750); err != nil {
		return fmt.Errorf("failed to setup helm directory: %v", err)
	}
	if err := util.VerboseCommand("helm", "--home", helm, "init", "--client-only").Run(); err != nil {
		return fmt.Errorf("failed to setup helm: %v", err)
	}

	allCharts := map[string][]string{
		"istio": {"install/kubernetes/helm/istio", "install/kubernetes/helm/istio-init"},
		"cni":   {"deployments/kubernetes/install/helm/istio-cni"},
	}
	for repo, charts := range allCharts {
		for _, chart := range charts {
			if err := sanitizeChart(manifest, path.Join(manifest.RepoDir(repo), chart)); err != nil {
				return fmt.Errorf("failed to sanitze chart %v: %v", chart, err)
			}
			// Package will create the tar.gz bundle for the given chart
			cmd := util.VerboseCommand("helm", "--home", helm, "package", chart, "--destination", path.Join(helm, "packages"))
			cmd.Dir = manifest.RepoDir(repo)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to package %v by `%v`: %v", chart, cmd.Args, err)
			}
		}
	}
	if err := util.VerboseCommand("helm", "--home", helm, "repo", "index", path.Join(helm, "packages")).Run(); err != nil {
		return fmt.Errorf("failed to create helm index.yaml")
	}

	// Copy to final charts/ output directory
	if err := util.CopyDir(path.Join(manifest.WorkDir(), "helm", "packages"), path.Join(manifest.OutDir(), "charts")); err != nil {
		return fmt.Errorf("failed to package helm chart: %v", err)
	}
	return nil
}

// In the final published charts, we need the version and tag to be set for the appropriate version
// In order to do this, we simply replace the current version with the new one.
// Currently the current version is not explicitly a placeholder - see https://github.com/istio/istio/issues/17146.
func sanitizeChart(manifest model.Manifest, s string) error {
	// TODO improve this to not use raw string handling of yaml
	currentVersion, err := ioutil.ReadFile(path.Join(s, "Chart.yaml"))
	if err != nil {
		return err
	}

	chart := make(map[string]interface{})
	if err := yaml.Unmarshal(currentVersion, &chart); err != nil {
		log.Errorf("unmarshal failed for Chart.yaml: %v", string(currentVersion))
		return fmt.Errorf("failed to unmarshal chart: %v", err)
	}

	// Getting the current version is a bit of a hack, we should have a more explicit way to handle this
	cv := chart["appVersion"].(string)
	if err := filepath.Walk(s, func(p string, info os.FileInfo, err error) error {
		fname := path.Base(p)
		if fname == "Chart.yaml" || fname == "values.yaml" {
			read, err := ioutil.ReadFile(p)
			if err != nil {
				return err
			}
			contents := string(read)
			// These fields contain the version, we swap out the placeholder with the correct version
			for _, replacement := range []string{"appVersion", "version", "tag"} {
				before := fmt.Sprintf("%s: %s", replacement, cv)
				after := fmt.Sprintf("%s: %s", replacement, manifest.Version)
				contents = strings.ReplaceAll(contents, before, after)
			}

			// The hub should also be updated
			before := fmt.Sprintf("%s: %s", "hub", "gcr.io/istio-release")
			after := fmt.Sprintf("%s: %s", "hub", manifest.Docker)
			contents = strings.ReplaceAll(contents, before, after)

			err = ioutil.WriteFile(p, []byte(contents), 0)
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}