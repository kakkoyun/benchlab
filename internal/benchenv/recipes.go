package benchenv

import "fmt"

// buildRecipes generates platform-specific benchmark commands for each usable
// path. Recipes are only emitted when the path is usable (not unavailable).
func buildRecipes(plat Platform, dkr Docker, readiness Readiness, numCPU int) Recipes {
	var recipes Recipes

	// Native recipe: emitted when the native path is usable.
	if readiness.Native != GradeUnavailable {
		recipes.Native = nativeRecipe(plat, numCPU)
	}

	// Docker recipe: emitted when the Docker path is usable and local.
	if dkr.Available && dkr.Local && readiness.Docker != GradeUnavailable {
		recipes.Docker = dockerRecipe(plat, dkr)
	}

	return recipes
}

// nativeRecipe returns the native benchmark command for the platform.
func nativeRecipe(plat Platform, numCPU int) string {
	switch plat.OS {
	case "linux":
		return "taskset -c 0 perflock go test -bench=. -benchmem -count=10 -benchtime=2s ./..."
	case "darwin":
		return "# macOS has a higher noise floor — use more samples to compensate\n" +
			"go test -bench=. -benchmem -count=20 -benchtime=2s ./..."
	default:
		return fmt.Sprintf("# %s native benchmarking is limited; use a Linux bare-metal runner\ngo test -bench=. -benchmem -count=10 -benchtime=2s ./...", plat.OS)
	}
}

// dockerRecipe returns the local Docker benchmark command with verified
// isolation limits.
func dockerRecipe(plat Platform, dkr Docker) string {
	arch := dkr.EngineArch
	if arch == "" {
		arch = plat.Arch
	}

	cpu := "0"
	if dkr.Isolation != nil && dkr.Isolation.SelectedCPU != "" {
		cpu = dkr.Isolation.SelectedCPU
	}

	memory := "512m"

	return fmt.Sprintf(`docker run --rm --network=none \
  --platform=linux/%s \
  --cpuset-cpus=%s \
  --cpus=1 \
  --memory=%s \
  --memory-swap=%s \
  -v $(pwd):/work -w /work \
  golang:1.24 \
  go test -bench=. -benchmem -count=10 -benchtime=2s ./...`, arch, cpu, memory, memory)
}
