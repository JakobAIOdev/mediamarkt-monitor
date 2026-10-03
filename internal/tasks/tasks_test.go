package tasks

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestTasksCSVRegionAndDuplicates(t *testing.T) {
	tasks, err := readTasksCSV(strings.NewReader("\ufeffregion,pid,note\nAT,2087300,box\nDE,2087300,other region\nat,2087300,duplicate\n"))
	want := []Task{{PID: "2087300", Region: "at"}, {PID: "2087300", Region: "de"}}
	if err != nil || !reflect.DeepEqual(tasks, want) {
		t.Fatalf("tasks=%v, err=%v, want=%v", tasks, err, want)
	}
}

func TestTasksCSVWebhookOverridesAndDeduplication(t *testing.T) {
	webhook := "https://discord.com/api/webhooks/123/test-secret"
	input := "region,webhook_url,pid\nAT,,2087300\nat, " + webhook + " ,2087300\nDE,,2087300\n"
	tasks, err := readTasksCSV(strings.NewReader(input))
	want := []Task{{PID: "2087300", Region: "at", WebhookURL: webhook}, {PID: "2087300", Region: "de"}}
	if err != nil || !reflect.DeepEqual(tasks, want) {
		t.Fatal("CSV overrides were not merged into one task per region/PID")
	}
	encoded, err := json.Marshal(tasks)
	if err != nil || strings.Contains(string(encoded), "test-secret") {
		t.Fatal("task JSON exposed a webhook token")
	}
}

func TestTasksCSVConflictingWebhooksDoNotLeakURLs(t *testing.T) {
	input := "pid,region,webhook_url\n2087300,AT,https://discord.com/api/webhooks/123/first-secret\n2087300,at,https://discord.com/api/webhooks/456/second-secret\n"
	_, err := readTasksCSV(strings.NewReader(input))
	if err == nil || !strings.Contains(err.Error(), "at/2087300") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "https://") {
		t.Fatal("conflicting destinations must fail without exposing URLs")
	}
}

func TestTasksCSVRejectsInvalidRows(t *testing.T) {
	for _, input := range []string{
		"", "pid,country\n2087300,AT\n", "pid,region,pid\n2087300,AT,2087300\n",
		"pid,region\n", "pid,region\n2087300,US\n", "pid,region\nnot-a-pid,AT\n",
		"pid,region\n2087300\n", "pid,region\n,AT\n",
	} {
		if _, err := readTasksCSV(strings.NewReader(input)); err == nil {
			t.Errorf("accepted invalid CSV %q", input)
		}
	}
}
