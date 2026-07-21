//go:build mage

// This repo intentionally has no Makefile and no .sh scripts. Every command
// this project needs is a Mage target, built on nirantaraai/nava's typed
// runners (github.com/nirantaraai/nava) — the same tool this org's other
// repos (e.g. nirantaraai/nava itself) use for build automation. Options
// live in go.yaml / docker.yaml / loadgen/*.yaml, not hardcoded in Go.
//
// Usage:
//
//	go install github.com/magefile/mage@latest
//	mage -l               # list every target
//	mage go:build          # build ./cmd/api -> bin/api (local dev binary)
//	mage go:crossBuild     # build ./cmd/api -> dist/linux_{amd64,arm64}/api (for Docker)
//	mage docker:up         # cross-builds, then docker compose up --build -d
//	mage loadgen:normal    # send 20 healthy requests
package main

import (
	"fmt"

	"github.com/magefile/mage/mg"

	dockermagex "github.com/nirantaraai/nava/mage/docker"
	gomagex "github.com/nirantaraai/nava/mage/golang"
	helmmagex "github.com/nirantaraai/nava/mage/helm"
	k3dmagex "github.com/nirantaraai/nava/mage/k3d"
	k8smagex "github.com/nirantaraai/nava/mage/k8s"
	sopsmagex "github.com/nirantaraai/nava/mage/sops"
	k8sx "github.com/nirantaraai/nava/pkg/k8s"
)

const k3dConfigPath = "self-hosted/k3d.yaml"

func init() {
	_ = gomagex.LoadConfig("go.yaml")
	_ = dockermagex.LoadConfig("docker.yaml")
	// k3d/sops config is only needed for cluster:*, helm:*, sops:* and deploy:* targets.
	// Silently skip if the file is missing so Go/Docker/Loadgen targets still work without
	// the self-hosted/ directory being present.
	_ = k3dmagex.LoadConfig(k3dConfigPath)
	_ = sopsmagex.LoadConfig(k3dConfigPath)
}

// Go namespace: local Go developer workflow (setup, build, run, test).
type Go mg.Namespace

// Setup downloads and tidies Go module dependencies.
func (Go) Setup() error { return gomagex.Setup() }

// Build compiles cmd/api into bin/api (CGO disabled, per go.yaml).
func (Go) Build() error { return gomagex.Build() }

// Run runs cmd/api locally with `go run` (Ctrl+C stops it gracefully).
func (Go) Run() error { return gomagex.Run() }

// Test runs the full unit test suite.
func (Go) Test() error { return gomagex.Test() }

// Vet runs `go vet ./...`.
func (Go) Vet() error { return gomagex.Vet() }

// CrossBuild cross-compiles cmd/api for linux/amd64 and linux/arm64 into
// dist/<os>_<arch>/api, per go.yaml's crossBuild section — this is what
// Dockerfile's `COPY dist/linux_${TARGETARCH}/api` expects to find. Same
// convention as this org's other Go CLIs (e.g. sh-mcp-go).
func (Go) CrossBuild() error { return gomagex.CrossBuild() }

// Docker namespace: the local SigNoz-backend + app stack (docker-compose.yml).
type Docker mg.Namespace

// Up builds and starts ClickHouse + the SigNoz OTel Collector + the app,
// detached. See docker-compose.yml's header comment for what it does and
// does not provision (the SigNoz app/UI itself is installed separately via
// Foundry — see README.md). Depends on Go.CrossBuild so the Dockerfile's
// prebuilt-binary COPY always has a fresh dist/linux_<arch>/api to copy.
func (Docker) Up() error {
	mg.Deps(Go.CrossBuild)
	return dockermagex.ComposeUp()
}

// Down stops and removes the stack's containers (data volumes are kept).
func (Docker) Down() error { return dockermagex.ComposeDown() }

// Build rebuilds the app image without starting anything. Depends on
// Go.CrossBuild — see Up.
func (Docker) Build() error {
	mg.Deps(Go.CrossBuild)
	return dockermagex.ComposeBuild()
}

// BuildxBuild builds the multi-arch (linux/amd64, linux/arm64) publishable
// image (config: docker.yaml -> buildxBuild), same pattern as sh-mcp-go.
// Depends on Go.CrossBuild for the same reason Up/Build do.
func (Docker) BuildxBuild() error {
	mg.Deps(Go.CrossBuild)
	return dockermagex.BuildxBuild()
}

// Push pushes the image built by BuildxBuild to the registry (config:
// docker.yaml -> push).
func (Docker) Push() error { return dockermagex.Push() }

// BuildAndPush cross-compiles, builds the multi-arch image with buildx, and
// pushes it to the registry in one shot. Used by CI (docker.yaml ->
// buildxBuild.push must be true). Equivalent to running BuildxBuild then Push,
// but avoids the separate `docker push` step that fails when the image was
// built with --push and never loaded into the local daemon.
func (Docker) BuildAndPush() error {
	mg.Deps(Go.CrossBuild)
	return dockermagex.BuildxBuild()
}

// Login logs in to the container registry (config: docker.yaml -> login).
func (Docker) Login() error { return dockermagex.Login() }

// Loadgen namespace: cmd/loadgen scenarios, each driven by its own
// loadgen/*.yaml — see Phase 5 of the project spec ("Generate Interesting
// Production Scenarios"). Run `mage docker:up` (or `mage go:run` locally)
// first so there's a service listening.
type Loadgen mg.Namespace

// Normal sends 20 sequential, healthy requests.
func (Loadgen) Normal() error { return runLoadgen("loadgen/loadgen-normal.yaml") }

// Slow sends requests that hit the injected SQLite latency.
func (Loadgen) Slow() error { return runLoadgen("loadgen/loadgen-slow.yaml") }

// Errors sends a mix of use-case and repository-level simulated failures.
func (Loadgen) Errors() error { return runLoadgen("loadgen/loadgen-errors.yaml") }

// Concurrent sends a 10-worker burst of mixed traffic.
func (Loadgen) Concurrent() error { return runLoadgen("loadgen/loadgen-concurrent.yaml") }

// Full runs every scenario in one shot (normal → slow → error → db-fail →
// list-orders → get-by-id → concurrent burst) to populate every panel in the
// SigNoz Order Service Overview dashboard without having to run each target
// individually. Equivalent to running all other Loadgen targets back-to-back.
func (Loadgen) Full() error { return runLoadgen("loadgen/loadgen-full.yaml") }

func runLoadgen(configPath string) error {
	runner, err := gomagex.NewRunnerFromYAML(configPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", configPath, err)
	}
	return runner.RunFromConfig()
}

// Cluster namespace: k3d cluster lifecycle for the self-hosted SigNoz stack.
type Cluster mg.Namespace

// Create creates the k3d cluster.
func (Cluster) Create() error { return k3dmagex.ClusterCreate() }

// Delete deletes the k3d cluster.
func (Cluster) Delete() error { return k3dmagex.ClusterDelete() }

// List lists k3d clusters.
func (Cluster) List() error { return k3dmagex.ClusterList() }

// Bootstrap creates namespaces and secrets.
func (Cluster) Bootstrap() error { return k3dmagex.Bootstrap() }

// Up creates cluster, bootstraps, and installs all releases.
func (Cluster) Up() error { return k3dmagex.Up() }

// Down tears down the cluster.
func (Cluster) Down() error { return k3dmagex.Down() }

// Status shows pods, PVCs, and ingresses.
func (Cluster) Status() error { return k3dmagex.Status() }

// Hosts prints access URLs for all services.
func (Cluster) Hosts() error {
	fmt.Println("Access the services at (open in your browser):")
	fmt.Println()
	fmt.Println("SigNoz UI:      http://signoz.127.0.0.1.nip.io")
	fmt.Println("Order Service:  http://signoz-demo.127.0.0.1.nip.io")
	fmt.Println()
	return nil
}

// Helm namespace: Helm chart management for the self-hosted SigNoz stack.
type Helm mg.Namespace

// Repos adds and updates all required Helm repositories.
func (Helm) Repos() error { return k3dmagex.HelmRepos() }

// InstallIngressNginx installs ingress-nginx via Helm.
func (Helm) InstallIngressNginx() error { return k3dmagex.InstallRelease("ingress-nginx") }

// InstallSignoz installs SigNoz into the cluster via the official SigNoz Helm
// chart (https://charts.signoz.io).
//
// Run after `mage cluster:up` (cluster + ingress-nginx must be ready first).
// All release options are in self-hosted/apps/local/signoz/helm.yaml.
func (Helm) InstallSignoz() error {
	h, err := helmmagex.NewRunnerFromYAML("self-hosted/apps/local/signoz/helm.yaml")
	if err != nil {
		return err
	}
	if err := h.RepoAdd(); err != nil {
		fmt.Printf("  (repo add: %v — continuing)\n", err)
	}
	if err := h.RepoUpdate(); err != nil {
		return err
	}
	return h.Upgrade()
}

// UninstallSignoz uninstalls the SigNoz Helm release.
// All release options are in self-hosted/apps/local/signoz/helm.yaml.
func (Helm) UninstallSignoz() error {
	h, err := helmmagex.NewRunnerFromYAML("self-hosted/apps/local/signoz/helm.yaml")
	if err != nil {
		return err
	}
	return h.Uninstall()
}

// ApplySignozIngress applies the standalone Ingress manifest that exposes the
// SigNoz UI at http://signoz.127.0.0.1.nip.io via ingress-nginx.
//
// Run after `mage helm:installSignoz` — ingress-nginx must be ready first.
// This is a separate step because the Ingress lives outside the Helm chart and
// must be kubectl-applied rather than Helm-managed.
func (Helm) ApplySignozIngress() error {
	ingressFile := "self-hosted/apps/local/signoz/ingress.yaml"
	fmt.Printf("Applying SigNoz ingress manifest: %s\n", ingressFile)
	return k8smagex.Apply(k8sx.ApplyOptions{
		Filenames: []string{ingressFile},
	})
}

// InstallK8sInfra installs the SigNoz k8s-infra Helm chart, which deploys an
// OpenTelemetry Collector DaemonSet that collects pod logs, host metrics,
// kubelet metrics, cluster metrics, and Kubernetes events — forwarding them
// all to the SigNoz ingester running inside the same cluster.
//
// Run AFTER `mage helm:installSignoz` (SigNoz must be ready to receive data).
// All release options are in self-hosted/apps/local/observability/k8s-infra/helm.yaml.
func (Helm) InstallK8sInfra() error {
	h, err := helmmagex.NewRunnerFromYAML("self-hosted/apps/local/observability/k8s-infra/helm.yaml")
	if err != nil {
		return err
	}
	if err := h.RepoAdd(); err != nil {
		fmt.Printf("  (repo add: %v — continuing)\n", err)
	}
	if err := h.RepoUpdate(); err != nil {
		return err
	}
	return h.Upgrade()
}

// UninstallK8sInfra uninstalls the k8s-infra Helm release.
// All release options are in self-hosted/apps/local/observability/k8s-infra/helm.yaml.
func (Helm) UninstallK8sInfra() error {
	h, err := helmmagex.NewRunnerFromYAML("self-hosted/apps/local/observability/k8s-infra/helm.yaml")
	if err != nil {
		return err
	}
	return h.Uninstall()
}

// Sops namespace: SOPS/age secret management for the self-hosted stack.
type Sops mg.Namespace

// Init installs sops+age, generates age key, updates .sops.yaml, and encrypts all secrets.
func (Sops) Init() error { return sopsmagex.Init() }

// Encrypt encrypts all .dec.yaml secret files to .enc.yaml.
func (Sops) Encrypt() error { return sopsmagex.Encrypt() }

// Decrypt decrypts all .enc.yaml secret files to .dec.yaml on disk.
func (Sops) Decrypt() error { return sopsmagex.Decrypt() }

// Deploy namespace: Kubernetes deployment of the Order Service.
type Deploy mg.Namespace

// SignozDemo deploys the signoz-demo Order Service into the cluster
// using the kustomize overlay at deploy/overlays/local.
// Make sure the image has been published to GHCR first (mage docker:push).
func (Deploy) SignozDemo() error {
	kustomizePath := "deploy/overlays/local"
	fmt.Printf("Deploying signoz-demo from: %s\n", kustomizePath)
	return k8smagex.Apply(k8sx.ApplyOptions{
		Kustomize: kustomizePath,
	})
}

// SignozDemoRollout waits for the signoz-demo rollout to complete.
func (Deploy) SignozDemoRollout() error {
	fmt.Println("Waiting for signoz-demo rollout...")
	return k8smagex.RolloutStatus("deployment/signoz-demo", "signoz-demo", "120s")
}

// K8sInfraStatus shows the status of the k8s-infra pods.
func (Deploy) K8sInfraStatus() error {
	return k8smagex.Get("pods", "", "k8s-infra")
}

// SignozStatus shows the status of the SigNoz pods.
func (Deploy) SignozStatus() error {
	return k8smagex.Get("pods", "", "signoz")
}
