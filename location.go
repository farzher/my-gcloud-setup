package main

type locationChoice struct {
	Label  string
	Place  string
	Region string
}

var freeLocations = []locationChoice{
	{Label: "East Coast", Place: "South Carolina", Region: "us-east1"},
	{Label: "Central", Place: "Iowa", Region: "us-central1"},
	{Label: "West Coast", Place: "Oregon", Region: "us-west1"},
}

func zoneForRegion(region string) string {
	switch region {
	case "us-east1", "us-central1", "us-west1":
		return region + "-b"
	default:
		return ""
	}
}
