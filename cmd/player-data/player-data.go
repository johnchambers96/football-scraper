package main

import (
	"encoding/json"
	"fmt"
	"football-data/pkg/s3"
	downloadFile "football-data/pkg/utils"
	"io"
	"log"
	"os"
	"sort"
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
	Rating    string   `json:"rating"`
	KitNumber int      `json:"kitNumber"`
}

type PagesToScrape struct {
	teamId int
	url    string
}

func main() {
	godotenv.Load("../../.env")

	// Get array of teams from file system
	data := getTeamsData()
	// Scrape player data from ECFC career mode website
	players := scrapeAllData(data)
	// players := getPlayersData()

	// Sort players by rating
	sortPlayersByRating(players)

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
	collectedPages := []PagesToScrape{}

	for _, league := range leagues.Leagues {
		for _, team := range league.Teams {
			time.Sleep(1 * time.Second)
			page := scrapePlayerUrls(team.Id)
			fmt.Println(page)
			collectedPages = append(collectedPages, page...)
		}
	}

	for _, page := range collectedPages {
		time.Sleep(1 * time.Second)
		playerData := scrapeData(page)
		collectedPlayerData = append(collectedPlayerData, playerData)
	}

	return collectedPlayerData
}

func scrapeData(page PagesToScrape) Player {
	var playerData = Player{}

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
		Delay: 5 * time.Second,
		//  // Add an additional random delay
		RandomDelay: 10 * time.Second,
		Parallelism: 2,
	})

	c.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36"

	// Fetch individual player data
	c.OnHTML("body", func(e *colly.HTMLElement) {
		if strings.Contains(e.Request.URL.Path, "player") {
			var playerPos = []string{}

			playerData.KnownName = e.ChildText(".profile h1")
			playerData.ShortName = e.ChildText("section .ellipsis")
			playerData.ImageUrl = e.ChildAttr(".profile img", "data-src")
			playerData.Rating = e.ChildAttr("article .grid .col:first-of-type em", "title")
			playerData.ClubId = page.teamId

			e.ForEach(".grid.attribute .col:nth-child(3) p:nth-child(6)", func(_ int, e *colly.HTMLElement) {
				if strings.Contains(e.Text, "Kit number") {
					kitNum, err := strconv.Atoi(strings.Split(e.Text, "Kit number ")[1])
					if err != nil {
						panic(err)
					} else {
						playerData.KitNumber = kitNum
					}
				}
			})

			playerId, err := strconv.Atoi(strings.Split(e.Request.URL.Path, "/")[2])
			if err != nil {
				panic(err)
			} else {
				playerData.Id = playerId
			}

			nationId, err := strconv.Atoi(strings.Split(e.ChildAttr(".profile a", "href"), "=")[1])
			if err != nil {
				panic(err)
			} else {
				playerData.NationId = nationId
			}

			e.ForEach(".profile .pos", func(_ int, e *colly.HTMLElement) {
				playerPos = append(playerPos, e.DOM.Text())
			})

			playerData.Positions = playerPos
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		fmt.Println("Request URL:", r.Request.URL)
	})

	c.Visit(page.url)

	c.Wait()

	fmt.Println(playerData.KnownName)

	return playerData
}

func scrapePlayerUrls(teamId int) []PagesToScrape {
	var baseUrl = "https://sofifa.com"

	var pagesToScrape = []PagesToScrape{}

	pageToScrape := baseUrl + "/team/" + strconv.Itoa(teamId)

	c := colly.NewCollector(
		// colly.Debugger(&debug.LogDebugger{}),
		colly.Async(true),
	)

	//Ignore the robot.txt
	c.IgnoreRobotsTxt = true
	// Time-out after 20 seconds.
	c.SetRequestTimeout(120 * time.Second)
	//use random agents during requests
	extensions.RandomUserAgent(c)

	//set limits to colly opoeration
	c.Limit(&colly.LimitRule{
		//  Filter domains affected by this rule
		DomainGlob: "https://sofifa.com/*",
		//  Set a delay between requests to these domains
		Delay: 5 * time.Second,
		//  Add an additional random delay
		RandomDelay: 10 * time.Second,
		Parallelism: 1,
	})

	c.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36"

	// Get player urls of squad players
	c.OnHTML("tr.starting, tr.sub, tr.res", func(e *colly.HTMLElement) {
		href := e.ChildAttr("td:nth-child(2) a:first-child", "href")

		var page = PagesToScrape{
			teamId: teamId,
			url:    baseUrl + href + "?hl=en-US",
		}

		pagesToScrape = append(pagesToScrape, page)
	})

	c.OnError(func(r *colly.Response, err error) {
		fmt.Println("Request URL:", r.Request.URL)
	})

	c.Visit(pageToScrape)

	c.Wait()

	return pagesToScrape
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
		ImgSrc    string   `json:"img_src"`
		Rating    int      `json:"rating"`
		KitNumber int      `json:"kit_number"`
	}

	supabase := supa.CreateClient(os.Getenv("SUPABASE_URL"), os.Getenv("SUPABASE_KEY"))

	for _, player := range players {

		rating, err := strconv.Atoi(player.Rating)

		if err != nil {
			panic(err)
		}

		row := PlayerSupa{
			Id:        player.Id,
			ClubId:    player.ClubId,
			NationId:  player.NationId,
			ShortName: player.ShortName,
			KnownName: player.KnownName,
			Positions: player.Positions,
			ImgSrc:    player.ImageUrl,
			Rating:    rating,
			KitNumber: player.KitNumber,
		}
		var results []PlayerSupa
		err = supabase.DB.From("players").Upsert(row).Execute(&results)
		if err != nil {
			panic(err)
		}
		fmt.Println(results)
	}

	return nil
}

func sortPlayersByRating(players []Player) []Player {
	sort.Slice(players, func(i, j int) bool {
		iVal, iErr := strconv.Atoi(players[i].Rating)
		jVal, jErr := strconv.Atoi(players[j].Rating)

		if iErr != nil {
			panic(iErr)
		}

		if jErr != nil {
			panic(jErr)
		}

		return iVal > jVal
	})

	return players
}
