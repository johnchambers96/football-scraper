package main

import (
	"encoding/json"
	"fmt"
	"football-data/pkg/s3"
	downloadFile "football-data/pkg/utils"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly"
	"github.com/gocolly/colly/extensions"
	"github.com/joho/godotenv"
	supa "github.com/nedpals/supabase-go"
)

type Teams struct {
	Id int `json:"id"`
}

type Leagues struct {
	Id    int     `json:"id"`
	Teams []Teams `json:"teams"`
}

type Data struct {
	Leagues []Leagues `json:"leagues"`
}

type Player struct {
	ShortName string   `json:"shortName"`
	KnownName string   `json:"knownName"`
	ImageUrl  string   `json:"imageUrl"`
	Id        int      `json:"id"`
	NationId  int      `json:"nationId"`
	ClubId    int      `json:"clubId"`
	Positions []string `json:"positions"`
}

func main() {
	godotenv.Load("../../.env")

	// Get array of teams from file system
	data := getTeamsData()
	// Scrape player data from ECFC career mode website
	players := scrapeAllData(data)
	// players := getPlayersData()

	// Download player images
	downloadPlayerImages(players)

	// Upload images to s3
	uploadPlayerImages()

	// Modify player data to use correct urls for images
	updatePlayerData(players)
	// Update/insert player data to mongodb
	insertPlayerData(players)

	saveDataToFile(players)
	fmt.Println("Player data finished.")
}

func saveDataToFile(data []Player) error {
	fmt.Println(data)
	content, err := json.Marshal(data)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(content)

	err = os.WriteFile("../../assets/players.json", content, 0644)
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

func getTeamsData() Data {
	jsonFile, err := os.Open("../../assets/data.json")
	if err != nil {
		panic(err)
	}

	defer jsonFile.Close()

	byteValue, _ := io.ReadAll(jsonFile)

	var result Data
	json.Unmarshal([]byte(byteValue), &result)

	return result
}

func getPlayersData() []Player {
	jsonFile, err := os.Open("../../assets/players.json")
	if err != nil {
		panic(err)
	}

	defer jsonFile.Close()

	byteValue, _ := io.ReadAll(jsonFile)

	var result []Player
	json.Unmarshal([]byte(byteValue), &result)

	return result
}

func scrapeAllData(leagues Data) []Player {

	collectedPlayerData := []Player{}

	for _, league := range leagues.Leagues {
		for _, team := range league.Teams {
			fmt.Println(team.Id)
			time.Sleep(3 * time.Second)
			playerData := scrapeData(team.Id)
			fmt.Println("done")
			collectedPlayerData = append(collectedPlayerData, playerData...)

		}
	}

	return collectedPlayerData
}

func scrapeData(teamId int) []Player {
	var baseUrl = "https://sofifa.com"

	var playersData []Player

	pageToScrape := baseUrl + "/team/" + strconv.Itoa(teamId)

	c := colly.NewCollector()

	//Ignore the robot.txt
	c.IgnoreRobotsTxt = true
	// Time-out after 20 seconds.
	c.SetRequestTimeout(20 * time.Second)
	//use random agents during requests
	extensions.RandomUserAgent(c)

	//set limits to colly opoeration
	c.Limit(&colly.LimitRule{
		//  // Filter domains affected by this rule
		DomainGlob: "https://sofifa.com/*",
		//  // Set a delay between requests to these domains
		Delay: 1 * time.Second,
		//  // Add an additional random delay
		RandomDelay: 3 * time.Second,
	})

	c.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36"

	// Fetch individual player data
	c.OnHTML("body", func(e *colly.HTMLElement) {
		if strings.Contains(e.Request.URL.Path, "player") {
			var playerData = Player{}
			var playerPos = []string{}

			playerData.KnownName = e.ChildText(".player .info h1")
			playerData.ShortName = e.ChildText(".header .ellipsis")
			playerData.ImageUrl = e.ChildAttr(".player img", "data-src")

			playerData.ClubId = teamId

			playerId, err := strconv.Atoi(strings.Split(e.Request.URL.Path, "/")[2])
			if err != nil {
				panic("test")
			} else {
				playerData.Id = playerId
			}

			nationId, err := strconv.Atoi(strings.Split(e.ChildAttr(".info a", "href"), "=")[1])
			if err != nil {
				panic("err")
			} else {
				playerData.NationId = nationId
			}

			e.ForEach(".info .pos", func(_ int, e *colly.HTMLElement) {
				playerPos = append(playerPos, e.DOM.Text())
			})

			playerData.Positions = playerPos

			playersData = append(playersData, playerData)
		}
	})

	// Get player urls of squad players
	c.OnHTML(".card:nth-of-type(2) .list tr", func(e *colly.HTMLElement) {
		href := e.ChildAttr(".col-name a", "href")
		c.Visit(baseUrl + href + "?hl=en-US")
	})

	c.Visit(pageToScrape)

	c.Wait()

	return playersData
}

func downloadPlayerImages(players []Player) error {
	for _, player := range players {
		err := downloadFile.DownloadFile("../../assets/players/"+strconv.Itoa(player.Id)+".png", player.ImageUrl)

		if err != nil {
			fmt.Println("Error downloading file: ", err)
			return nil
		}
	}
	return nil
}

func uploadPlayerImages() error {
	nationFiles, err := os.ReadDir("../../assets/players")
	if err != nil {
		log.Fatal(err)
	}

	var filePaths []string
	for _, file := range nationFiles {
		filePaths = append(filePaths, "../../assets/players/"+file.Name())
	}

	return s3.UploadImagesToS3(os.Getenv("BUCKET_NAME"), filePaths, "players/24/")
}

func updatePlayerData(players []Player) []Player {
	for i := range players {
		players[i].ImageUrl = "https://cdn.lineup-builder.co.uk/players/24/" + strconv.Itoa(players[i].Id) + ".png"
	}
	return players
}

func insertPlayerData(players []Player) error {
	type PlayerSupa struct {
		Id        int      `json:"id"`
		ClubId    int      `json:"club_id"`
		NationId  int      `json:"nation_id"`
		ShortName string   `json:"short_name"`
		KnownName string   `json:"known_name"`
		Positions []string `json:"positions"`
	}

	supabase := supa.CreateClient(os.Getenv("SUPABASE_URL"), os.Getenv("SUPABASE_KEY"))

	for _, player := range players {
		row := PlayerSupa{
			Id:        player.Id,
			ClubId:    player.ClubId,
			NationId:  player.NationId,
			ShortName: player.ShortName,
			KnownName: player.KnownName,
			Positions: player.Positions,
		}
		var results []PlayerSupa
		err := supabase.DB.From("players").Upsert(row).Execute(&results)
		if err != nil {
			panic(err)
		}
		fmt.Println(results)
	}

	return nil
}
