package main

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Reports are projections: never put a recipe, command error or rendered model here.
type operationCheck struct {
	Code        string `json:"code"`
	Status      string `json:"status"`
	Target      string `json:"target"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}
type operationReport struct {
	SchemaVersion int              `json:"schema_version"`
	Command       string           `json:"command"`
	Complete      bool             `json:"complete"`
	Data          any              `json:"data"`
	Checks        []operationCheck `json:"checks"`
}

func newReport(command string, data any) operationReport {
	return operationReport{1, command, true, data, []operationCheck{}}
}
func (r *operationReport) add(code, status, target, message string) {
	r.Checks = append(r.Checks, operationCheck{Code: code, Status: status, Target: target, Message: message})
}
func (r *operationReport) unavailable(code, target, message string) {
	r.Complete = false
	r.add(code, "fail", target, message)
}
func (r operationReport) exitCode() int {
	if !r.Complete {
		return 1
	}
	for _, c := range r.Checks {
		if c.Status == "fail" {
			return 1
		}
	}
	return 0
}
func (r operationReport) write(asJSON bool) (int, error) {
	sort.SliceStable(r.Checks, func(i, j int) bool {
		a, b := r.Checks[i], r.Checks[j]
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return a.Code < b.Code
	})
	if asJSON {
		return r.exitCode(), json.NewEncoder(stdout).Encode(r)
	}
	fmt.Fprintln(stdout, r.Command)
	for _, c := range r.Checks {
		fmt.Fprintf(stdout, "  %-4s %s [%s]: %s\n", c.Status, c.Code, c.Target, c.Message)
	}
	if r.Data != nil {
		b, err := json.MarshalIndent(r.Data, "  ", "  ")
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(stdout, string(b))
	}
	return r.exitCode(), nil
}
