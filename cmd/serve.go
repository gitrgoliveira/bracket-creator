package cmd

import (
	"fmt"
	"os"

	"bufio"
	"bytes"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/spf13/cobra"
)

type serveOptions struct {
	bindAddress string
	port        int
}

func newServeCmd() *cobra.Command {
	o := &serveOptions{}

	cmd := &cobra.Command{
		Use:          "serve",
		Short:        "serves a web gui",
		SilenceUsage: true,
		RunE:         o.run,
	}

	bindAddress := os.Getenv("BIND_ADDRESS")
	if bindAddress == "" {
		bindAddress = "localhost" // default value
	}
	cmd.Flags().StringVarP(&o.bindAddress, "bind", "b", bindAddress, "bind address")

	portStr := os.Getenv("PORT")
	port := 8080 // default value
	if portStr != "" {
		var err error
		port, err = strconv.Atoi(portStr)
		if err != nil {
			fmt.Println("Warning: Invalid PORT environment variable. Using default.")
			port = 8080
		}
	}
	cmd.Flags().IntVarP(&o.port, "port", "p", port, "port number")

	return cmd
}
func (o *serveOptions) run(cmd *cobra.Command, args []string) error {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.LoadHTMLFiles("./web/index.html")
	r.StaticFile("/logo.jpeg", "./web/logo.jpeg")

	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})

	r.POST("/", func(c *gin.Context) {
		text := c.PostForm("playerList")
		if text == "" {
			c.String(http.StatusBadRequest, "Empty player list")
			return
		}
		singleTree := c.PostForm("singleTree") == "on"
		sanatize := c.PostForm("sanatize") == "on"
		determined := c.PostForm("determined") == "on"
		teamMatches, _ := strconv.Atoi(c.PostForm("teamMatches"))
		tournamentType := c.PostForm("tournamentType")
		winnersPerPool, _ := strconv.Atoi(c.PostForm("winnersPerPool"))
		playersPerPool, _ := strconv.Atoi(c.PostForm("playersPerPool"))
		roundRobin := c.PostForm("roundRobin") == "on"

		inMemoryBuffer := new(bytes.Buffer)
		inMemoryWriter := bufio.NewWriter(inMemoryBuffer)
		if tournamentType == "pools" {
			o := &poolOptions{
				singleTree:  singleTree,
				sanatize:    sanatize,
				determined:  determined,
				teamMatches: teamMatches,
				roundRobin:  roundRobin,
				numPlayers:  playersPerPool,
				poolWinners: winnersPerPool,
			}
			o.outputWriter = inMemoryWriter

			err := o.createPools(strings.Split(text, "\n"))
			if err != nil {
				fmt.Printf("failed to create pools: %s", err.Error())
			}

		} else if tournamentType == "playoffs" {

			o := &playoffOptions{
				singleTree:  singleTree,
				sanatize:    sanatize,
				determined:  determined,
				teamMatches: teamMatches,
			}

			o.outputWriter = inMemoryWriter

			err := o.createPlayoffs(strings.Split(text, "\n"))
			if err != nil {
				fmt.Printf("failed to create playoffs: %s", err.Error())
			}
		}

		c.Header("Content-Description", "File Transfer")
		c.Header("Content-Transfer-Encoding", "binary")
		c.Header("Content-Disposition", "attachment; filename=output.xlsx")
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", inMemoryBuffer.Bytes())
	})

	err := r.Run(o.bindAddress + ":" + strconv.Itoa(o.port))
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(newServeCmd())
}
