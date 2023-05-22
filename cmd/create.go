package cmd

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/spf13/cobra"
)

type createOptions struct {
	numPlayers int
	filePath   string
}
type pool struct {
	teams []string
}

func defaultCreateOptions() *createOptions {
	return &createOptions{}
}
func newCreateCmd() *cobra.Command {
	o := defaultCreateOptions()

	cmd := &cobra.Command{
		Use:          "create",
		Short:        "subcommand to create brackets",
		SilenceUsage: true,
		// Args:         cobra.ExactArgs(1),
		RunE: o.run,
	}

	cmd.Flags().IntVarP(&o.numPlayers, "players", "", 3, "minimum number of players per pool")

	cmd.Flags().StringVarP(&o.filePath, "path", "p", "", "file with the list of players")

	return cmd
}

func (o *createOptions) run(cmd *cobra.Command, args []string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", o.filePath)

	file, err := os.Open(o.filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	entries := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		entry := scanner.Text()
		entries = append(entries, entry)
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(entries), func(i, j int) {
		entries[i], entries[j] = entries[j], entries[i]
	})

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file:", err)
		return nil
	}

	// pools := make([]map[int]string, 0)
	// currentPool := make(map[int]string)

	// max_pools := len(competitors) / o.numPlayers
	groups := make([]map[int]string, 0)
	currentGroup := make(map[int]string)

	// Arrange entries in groups of three
	for i, entry := range entries {
		currentGroup[i%o.numPlayers+1] = entry
		if (i+1)%o.numPlayers == 0 {
			groups = append(groups, currentGroup)
			currentGroup = make(map[int]string)
		}
	}
	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}
	for i, group := range groups {
		fmt.Printf("Group %d:\n", i+1)
		for _, entry := range group {
			fmt.Printf("%s\n", entry)
		}
		fmt.Println()
	}

	return nil
}

// func (o *exampleOptions) parseArgs(args []string) ([]int, error) {
// }

// createCmd represents the version command

func init() {
	rootCmd.AddCommand(newCreateCmd())
}
