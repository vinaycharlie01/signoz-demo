//go:build mage

// This repo intentionally has no Makefile and no .sh scripts. Every command
// this project needs is a Mage target, built on nirantaraai/nava's typed
// runners (github.com/nirantaraai/nava) — the same tool this org's other
// repos (e.g. nirantaraai/nava itself) use for build automation. Options
// live in go.yaml / docker.yaml / loadgen-*.yaml, not hardcoded in Go.
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
	"os"
	"os/exec"

	"github.com/magefile/mage/mg"

	dockermagex "github.com/nirantaraai/nava/mage/docker"
	gomagex "github.com/nirantaraai/nava/mage/golang"
	k3dmagex "github.com/nirantaraai/nava/mage/k3d"
	sopsmagex "github.com/nirantaraai/nava/mage/sops"
)

const k3dConfigPath = "self-hosted/k3d.yaml"

func init() {
	_ = gomagex.LoadConfig("go.yaml")
	_ = dockermagex.LoadConfig("docker.yaml")
	// k3d/sops config is only needed for cluster:*, helm:*, gitops:*, sops:* and deploy:* targets.
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

// Login logs in to the container registry (config: docker.yaml -> login).
func (Docker) Login() error { return dockermagex.Login() }

// Loadgen namespace: cmd/loadgen scenarios, each driven by its own
// loadgen-*.yaml — see Phase 5 of the project spec ("Generate Interesting
// Production Scenarios"). Run `mage docker:up` (or `mage go:run` locally)
// first so there's a service listening.
type Loadgen mg.Namespace

// Normal sends 20 sequential, healthy requests.
func (Loadgen) Normal() error { return runLoadgen("loadgen-normal.yaml") }

// Slow sends requests that hit the injected SQLite latency.
func (Loadgen) Slow() error { return runLoadgen("loadgen-slow.yaml") }

// Errors sends a mix of use-case and repository-level simulated failures.
func (Loadgen) Errors() error { return runLoadgen("loadgen-errors.yaml") }

// Concurrent sends a 10-worker burst of mixed traffic.
func (Loadgen) Concurrent() error { return runLoadgen("loadgen-concurrent.yaml") }

// Full runs every scenario in one shot (normal → slow → error → db-fail →
// list-orders → get-by-id → concurrent burst) to populate every panel in the
// SigNoz Order Service Overview dashboard without having to run each target
// individually. Equivalent to running all other Loadgen targets back-to-back.
func (Loadgen) Full() error { return runLoadgen("loadgen-full.yaml") }

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

// Status shows pods, PVCs, ingresses, and ArgoCD applications.
func (Cluster) Status() error { return k3dmagex.Status() }

// Hosts prints access URLs for all services.
func (Cluster) Hosts() error {
	fmt.Println("Access the services at (open in your browser):")
	fmt.Println()
	fmt.Println("SigNoz UI:      http://signoz.127.0.0.1.nip.io")
	fmt.Println("Order Service:  http://signoz-demo.127.0.0.1.nip.io")
	fmt.Println()
	fmt.Println("(optional — if installed)")
	fmt.Println("Grafana:        http://grafana.127.0.0.1.nip.io")
	fmt.Println("ArgoCD:         http://argocd.127.0.0.1.nip.io")
	return nil
}

// Helm namespace: Helm chart management for the self-hosted SigNoz stack.
type Helm mg.Namespace

// Repos adds and updates all required Helm repositories.
func (Helm) Repos() error { return k3dmagex.HelmRepos() }

// InstallGrafana installs Grafana via Helm.
func (Helm) InstallGrafana() error { return k3dmagex.InstallRelease("grafana") }

// InstallIngressNginx installs ingress-nginx via Helm.
func (Helm) InstallIngressNginx() error { return k3dmagex.InstallRelease("ingress-nginx") }

// InstallArgoCD installs ArgoCD via Helm.
func (Helm) InstallArgoCD() error { return k3dmagex.InstallArgoCD() }

// CreateRepoSecret creates ArgoCD secret for private git repo access.
func (Helm) CreateRepoSecret() error { return k3dmagex.CreateRepoSecret() }

// InstallSignoz installs SigNoz into the cluster via the official SigNoz Helm
// chart (https://charts.signoz.io).
//
// Run after `mage cluster:up` (cluster + ingress-nginx must be ready first).
// Override the values file with SIGNOZ_VALUES env var if needed.
func (Helm) InstallSignoz() error {
	valuesFile := os.Getenv("SIGNOZ_VALUES")
	if valuesFile == "" {
		valuesFile = "self-hosted/apps/local/signoz/values.yaml"
	}

	fmt.Println("Adding SigNoz Helm repo...")
	if err := runHelm("repo", "add", "signoz", "https://charts.signoz.io"); err != nil {
		fmt.Printf("  (repo add: %v — continuing)\n", err)
	}
	if err := runHelm("repo", "update"); err != nil {
		return err
	}

	fmt.Printf("Installing SigNoz Helm chart with values: %s\n", valuesFile)
	return runHelm(
		"upgrade", "--install",
		"signoz", "signoz/signoz",
		"--namespace", "signoz",
		"--create-namespace",
		"--values", valuesFile,
		"--timeout", "15m",
	)
}

// UninstallSignoz uninstalls the SigNoz Helm release.
func (Helm) UninstallSignoz() error {
	fmt.Println("Uninstalling SigNoz Helm release...")
	return runHelm("uninstall", "signoz", "--namespace", "signoz")
}

// InstallK8sInfra installs the SigNoz k8s-infra Helm chart, which deploys an
// OpenTelemetry Collector DaemonSet that collects pod logs, host metrics,
// kubelet metrics, cluster metrics, and Kubernetes events — forwarding them
// all to the SigNoz ingester running inside the same cluster.
//
// Run AFTER `mage helm:installSignoz` (SigNoz must be ready to receive data).
func (Helm) InstallK8sInfra() error {
	baseValues := "self-hosted/apps/base/observability/k8s-infra/values.yaml"
	envValues := "self-hosted/apps/local/observability/k8s-infra/values.yaml"

	fmt.Println("Installing k8s-infra Helm chart (pod log + metric collection)...")
	if err := runHelm("repo", "add", "signoz", "https://charts.signoz.io"); err != nil {
		fmt.Printf("  (repo add: %v — continuing)\n", err)
	}
	if err := runHelm("repo", "update"); err != nil {
		return err
	}

	return runHelm(
		"upgrade", "--install",
		"k8s-infra", "signoz/k8s-infra",
		"--namespace", "k8s-infra",
		"--create-namespace",
		"--values", baseValues,
		"--values", envValues,
		"--timeout", "10m",
	)
}

// UninstallK8sInfra uninstalls the k8s-infra Helm release.
func (Helm) UninstallK8sInfra() error {
	fmt.Println("Uninstalling k8s-infra Helm release...")
	return runHelm("uninstall", "k8s-infra", "--namespace", "k8s-infra")
}

// Gitops namespace: ArgoCD GitOps workflow for the self-hosted SigNoz stack.
type Gitops mg.Namespace

// Apply applies the ArgoCD app-of-apps.
func (Gitops) Apply() error { return k3dmagex.ApplyAppOfApps() }

// Bootstrap creates cluster, installs ArgoCD, applies app-of-apps.
func (Gitops) Bootstrap() error { return k3dmagex.GitopsBootstrap() }

// PatchPrune patches ArgoCD applications to enable pruning.
func (Gitops) PatchPrune() error {
	names, err := k3dmagex.ApplicationsToPrune()
	if err != nil {
		return err
	}
	patch := `{"operation":{"sync":{"prune":true}}}`
	for _, name := range names {
		if err := k3dmagex.PatchApplication(name, "merge", patch); err != nil {
			return err
		}
	}
	return nil
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
	return runKubectl("apply", "-k", kustomizePath)
}

// SignozDemoRollout waits for the signoz-demo rollout to complete.
func (Deploy) SignozDemoRollout() error {
	fmt.Println("Waiting for signoz-demo rollout...")
	return runKubectl("rollout", "status", "deployment/signoz-demo",
		"-n", "signoz-demo", "--timeout=120s")
}

// K8sInfraStatus shows the status of the k8s-infra pods.
func (Deploy) K8sInfraStatus() error {
	return runKubectl("get", "pods", "-n", "k8s-infra")
}

// SignozStatus shows the status of the SigNoz pods.
func (Deploy) SignozStatus() error {
	return runKubectl("get", "pods", "-n", "signoz")
}

// ---------- helpers ----------

func runKubectl(args ...string) error {
	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runHelm(args ...string) error {
	cmd := exec.Command("helm", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
