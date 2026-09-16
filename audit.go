package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func auditFreeTier(ctx context.Context, cfg config, instance instanceInfo, staticIP string) []string {
	var warnings []string

	if machine := resourceName(instance.MachineType); machine != "" && machine != "e2-micro" {
		warnings = append(warnings, "machine type is "+machine)
	}
	if zoneForRegion(cfg.region()) == "" {
		warnings = append(warnings, "region "+cfg.region()+" is not Free Tier")
	}

	bootDisk := ""
	for _, disk := range instance.Disks {
		if disk.Boot {
			bootDisk = resourceName(disk.Source)
			break
		}
	}
	if bootDisk != "" {
		r, err := run(ctx, "gcloud", "compute", "disks", "describe", bootDisk,
			"--project="+cfg.Project, "--zone="+cfg.zone(), "--format=json(type,sizeGb)")
		if err == nil {
			var disk struct {
				Type   string `json:"type"`
				SizeGB string `json:"sizeGb"`
			}
			if json.Unmarshal([]byte(r.Stdout), &disk) == nil {
				if diskType := resourceName(disk.Type); diskType != "" && diskType != "pd-standard" {
					warnings = append(warnings, "boot disk is "+diskType)
				}
				if size, err := strconv.Atoi(disk.SizeGB); err == nil && size > 30 {
					warnings = append(warnings, fmt.Sprintf("boot disk is %d GB", size))
				}
			}
		}
	}

	if r, err := run(ctx, "gcloud", "compute", "instances", "list", "--project="+cfg.Project, "--format=value(name)"); err == nil {
		extras := 0
		for _, name := range uniqueLines(r.Stdout) {
			if name != vmName {
				extras++
			}
		}
		if extras > 0 {
			warnings = append(warnings, countWarning(extras, "extra VM", "extra VMs"))
		}
	}

	if r, err := run(ctx, "gcloud", "compute", "disks", "list", "--project="+cfg.Project, "--format=value(name)"); err == nil {
		extras := 0
		for _, name := range uniqueLines(r.Stdout) {
			if name != bootDisk {
				extras++
			}
		}
		if extras > 0 {
			warnings = append(warnings, countWarning(extras, "extra disk", "extra disks"))
		}
	}

	if r, err := run(ctx, "gcloud", "compute", "addresses", "list", "--project="+cfg.Project, "--format=json"); err == nil {
		var addresses []struct {
			Name   string `json:"name"`
			Region string `json:"region"`
			Status string `json:"status"`
		}
		if json.Unmarshal([]byte(r.Stdout), &addresses) == nil {
			extras := 0
			for _, address := range addresses {
				if address.Name == addressName && resourceName(address.Region) == cfg.region() {
					if strings.EqualFold(address.Status, "RESERVED") && staticIP != "" && instance.ip() != staticIP {
						warnings = append(warnings, "static IP is reserved but unused")
					}
					continue
				}
				extras++
			}
			if extras > 0 {
				warnings = append(warnings, countWarning(extras, "extra static IP", "extra static IPs"))
			}
		}
	}

	if r, err := run(ctx, "gcloud", "compute", "snapshots", "list", "--project="+cfg.Project, "--format=value(name)"); err == nil {
		if count := len(uniqueLines(r.Stdout)); count > 0 {
			warnings = append(warnings, countWarning(count, "snapshot", "snapshots"))
		}
	}
	if r, err := run(ctx, "gcloud", "compute", "machine-images", "list", "--project="+cfg.Project, "--format=value(name)"); err == nil {
		if count := len(uniqueLines(r.Stdout)); count > 0 {
			warnings = append(warnings, countWarning(count, "machine image", "machine images"))
		}
	}

	return warnings
}

func resourceName(value string) string {
	value = strings.TrimSpace(value)
	if i := strings.LastIndexByte(value, '/'); i >= 0 {
		return value[i+1:]
	}
	return value
}

func countWarning(count int, singular, plural string) string {
	if count == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", count, plural)
}
