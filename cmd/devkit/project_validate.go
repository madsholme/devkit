package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func currentRecipeRoot() string {
	root, _ := os.Getwd()
	for dir := root; ; dir = filepath.Dir(dir) {
		if isFile(configFile(dir)) {
			return resolve(dir)
		}
		if filepath.Dir(dir) == dir {
			return resolve(root)
		}
	}
}
func projectValidateCLI(args []string) (int, error) {
	positional, values, err := splitArgs(args, map[string]bool{"--runtime": true, "--vm": true}, map[string]bool{"--json": true})
	runtime := firstOf(values["--runtime"], "all")
	target, hasVM := values["--vm"]
	_, hasRuntime := values["--runtime"]
	if err != nil || len(positional) > 0 || (runtime != "all" && runtime != "containers" && runtime != "vms") || (hasVM && (target == "" || hasRuntime)) {
		return 2, usageError("Use project validate [--runtime all|containers|vms | --vm TARGET] [--json]")
	}
	if hasVM {
		runtime = "vms"
	}
	_, asJSON := values["--json"]
	r := newReport("project validate", map[string]any{"runtime": runtime})
	root := currentRecipeRoot()
	if !isFile(configFile(root)) {
		r.add("recipe.missing", "fail", "recipe", "No project recipe is available.")
		return r.write(asJSON)
	}
	cfg, err := loadConfig(root)
	if err != nil {
		r.add("recipe.invalid", "fail", "recipe", "Recipe syntax or fields are invalid; inspect the local recipe.")
		return r.write(asJSON)
	}
	r.add("recipe.valid", "ok", cfg.Project, "Recipe fields and selectors are valid.")
	if runtime != "vms" {
		validateContainers(&r, root, cfg, runtime == "containers")
	}
	if runtime != "containers" {
		validateProjectVMs(&r, root, cfg, target, runtime == "vms")
	}
	if len(cfg.Compose) == 0 && !isFile(filepath.Join(root, "compose.live.yaml")) && !isFile(filepath.Join(root, "compose.review.yaml")) && len(cfg.ExistingCompose) == 0 && len(cfg.VMs) == 0 {
		r.add("recipe.empty", "fail", cfg.Project, "Recipe has no configured component.")
	}
	return r.write(asJSON)
}
func validateContainers(r *operationReport, root string, cfg projectConfig, required bool) {
	preview := len(cfg.Compose) > 0 || isFile(filepath.Join(root, "compose.live.yaml")) || isFile(filepath.Join(root, "compose.review.yaml"))
	if !preview && len(cfg.ExistingCompose) == 0 {
		status := "skip"
		if required {
			status = "fail"
		}
		r.add("containers.absent", status, cfg.Project, "No container component is configured.")
		return
	}
	if preview {
		port, err := strconv.Atoi(cfg.webPort())
		if err != nil || port < 1 || port > 65535 || !serviceName.MatchString(cfg.webService()) {
			r.add("compose.web", "fail", cfg.Project, "Web service or port is invalid.")
		}
		for _, mode := range []string{"live", "review"} {
			source := firstOf(cfg.Compose[mode], "compose."+mode+".yaml")
			if !isFile(filepath.Join(root, source)) && cfg.Compose[mode] == "" && mode == "review" {
				r.add("compose.review", "warn", mode, "Review is unavailable; supply a review model and build hook to enable it.")
				continue
			}
			model, _, err := inspectCompose(root, cfg, mode, "devkit-validation", validationMediaEnv(cfg, mode))
			if err != nil {
				var unavailable inspectionUnavailable
				if errors.As(err, &unavailable) {
					r.Complete = false
				}
				r.add("compose.model", "fail", mode, "Cannot validate Compose inputs/model; check local paths, required interpolation, Compose availability and preview isolation constraints.")
				continue
			}
			if err := exposeMedia(object(model, "services"), cfg.MediaPorts, validationMediaEnv(cfg, mode)); err != nil {
				r.add("compose.media", "fail", mode, "Media mapping service or listener is invalid.")
			}
			r.add("compose.model", "ok", mode, "Compose model satisfies preview isolation constraints; runtime health is unverified.")
			if !composeHealth(model) {
				r.add("compose.health", "fail", mode, "Each service needs an enabled health check.")
			}
			if mode == "review" && cfg.Hooks["build"] == "" {
				r.add("compose.review-build", "fail", mode, "A supplied review model requires a build hook; no hook was executed.")
			}
		}
	}
	if len(cfg.ExistingCompose) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rows, err := hostContainers(ctx)
		if err != nil {
			r.unavailable("containers.inventory", cfg.Project, "Docker inventory is unavailable; registration ownership is unknown.")
			return
		}
		owners, err := existingContainerProjects(rows)
		if err != nil {
			r.unavailable("containers.ownership", cfg.Project, "Registration ownership is incomplete or conflicting.")
			return
		}
		for _, entry := range cfg.ExistingCompose {
			found := false
			for _, c := range rows {
				if c.Config.Labels["com.docker.compose.project"] == entry.Project && resolve(c.Config.Labels["com.docker.compose.project.working_dir"]) == resolve(filepath.Join(root, entry.WorkingDir)) {
					found = true
					if owners[c.ID].Root != resolve(root) {
						r.add("containers.ownership", "fail", entry.Project, "Container does not have a unique registered owner.")
					}
				}
			}
			if !found {
				r.add("containers.observed", "warn", entry.Project, "No matching container was observed; registration does not create containers.")
			} else {
				r.add("containers.observed", "ok", entry.Project, "Matching registered containers were observed.")
			}
		}
	}
}
