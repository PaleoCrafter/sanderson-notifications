package plugins

import (
	"17thshard.com/sanderson-notifications/common"
	"encoding/json"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type ProgressPlugin struct {
	Url     string
	Message string
}

func (plugin ProgressPlugin) Name() string {
	return "twitter"
}

func (plugin ProgressPlugin) Validate() error {
	if len(plugin.Url) == 0 {
		return fmt.Errorf("URL for progress updates must not be empty")
	}

	if len(plugin.Message) == 0 {
		return fmt.Errorf("message for progress updates must not be empty")
	}

	return nil
}

type Progress struct {
	Title string
	Link  string
	Value int
}

type ProgressDiff struct {
	Title    string
	Link     string
	OldValue int
	Value    int
	New      bool
}

const (
	blockSize  = 2.5
	blockCount = 100 / blockSize
)

func (plugin ProgressPlugin) Check(context PluginContext) error {
	context.Info.Println("Checking for progress updates...")

	res, err := http.Get(plugin.Url)
	if err != nil {
		return fmt.Errorf("could not read progress site '%s': %w", plugin.Url, err)
	}
	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return err
	}

	oldProgress, err := readOldProgress()
	if err != nil {
		return err
	}

	currentProgress, err := readProgress(doc)
	if err != nil {
		return err
	}

	differences := diff(oldProgress, currentProgress)

	if differences == nil {
		context.Info.Println("No progress changes to report.")
		return nil
	}

	context.Info.Println("Reporting changed progress bars...")

	plugin.reportProgress(context.Discord, differences)

	err = persistProgress(currentProgress)
	if err != nil {
		return err
	}
	return nil
}

func readOldProgress() ([]Progress, error) {
	content, err := ioutil.ReadFile("last_progress.json")
	if os.IsNotExist(err) {
		content = []byte("[]")
	}

	var oldProgress []Progress
	err = json.Unmarshal(content, &oldProgress)
	if err != nil {
		return nil, err
	}

	return oldProgress, nil
}

func readProgress(doc *goquery.Document) ([]Progress, error) {
	bars := doc.Find(".vc_progress_bar .vc_label")
	result := make([]Progress, bars.Length())

	if bars.Length() == 0 {
		html, _ := doc.Html()
		return nil, fmt.Errorf("Unexpectedly received empty list of progress bars, content was %s", html)
	}

	bars.Each(func(i int, selection *goquery.Selection) {
		title := strings.TrimSpace(selection.Contents().Not("span").Text())
		link := selection.Find("a").AttrOr("href", "")
		value := selection.NextFiltered(".vc_single_bar").Find(".vc_bar").AttrOr("data-percentage-value", "0")

		parsedValue, _ := strconv.Atoi(value)

		result[i] = Progress{title, link, parsedValue}
	})

	return result, nil
}

func diff(old, new []Progress) []ProgressDiff {
	result := make([]ProgressDiff, len(new), len(new))
	oldKeyed := make(map[string]Progress)

	for _, v := range old {
		oldKeyed[v.Title] = v
	}

	noChanges := true
	for i, v := range new {
		existing, existedBefore := oldKeyed[v.Title]

		oldValue := 0
		if existedBefore {
			oldValue = existing.Value
		}

		result[i] = ProgressDiff{
			v.Title,
			v.Link,
			oldValue,
			v.Value,
			!existedBefore,
		}

		if !existedBefore || oldValue != v.Value {
			noChanges = false
		}
	}

	if noChanges {
		return nil
	}

	return result
}

func (plugin ProgressPlugin) reportProgress(client *common.DiscordClient, progressBars []ProgressDiff) {
	var embedBuilder strings.Builder

	for i, progress := range progressBars {
		if i != 0 {
			embedBuilder.WriteString("\n\n")
		}

		title := progress.Title
		if len(progress.Link) > 0 {
			title = fmt.Sprintf("[%s](%s)", progress.Title, progress.Link)
		}
		if progress.New {
			title = fmt.Sprintf("[New] %s", title)
		} else if progress.Value != progress.OldValue {
			title = fmt.Sprintf("[Changed] %s (%d%% → %d%%)", title, progress.OldValue, progress.Value)
		}
		embedBuilder.WriteString(fmt.Sprintf("**%s**\n", title))

		fullBlocks := int(math.Floor(float64(progress.Value) / blockSize))
		embedBuilder.WriteRune('`')
		embedBuilder.WriteString(strings.Repeat("█", fullBlocks))
		embedBuilder.WriteString(strings.Repeat("░", blockCount-fullBlocks))
		embedBuilder.WriteString(fmt.Sprintf(" %3d%%", progress.Value))
		embedBuilder.WriteRune('`')
	}

	embed := map[string]interface{}{
		"description": embedBuilder.String(),
		"footer": map[string]interface{}{
			"text": fmt.Sprintf("See %s for more", plugin.Url),
		},
	}

	client.Send(
		plugin.Message,
		"Progress Updates",
		"dragonsteel",
		embed,
	)
}

func persistProgress(progress []Progress) error {
	content, _ := json.Marshal(progress)

	return ioutil.WriteFile("last_progress.json", content, 0644)
}
