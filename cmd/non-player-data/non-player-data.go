package main

import (
	"encoding/json"
	"fmt"
	"football-data/pkg/s3"
	downloadFile "football-data/pkg/utils"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/joho/godotenv"
	supa "github.com/nedpals/supabase-go"
)

type Gender struct {
	Id    int    `json:"id"`
	Label string `json:"label"`
}

type Nationality struct {
	Id       int    `json:"id"`
	Label    string `json:"label"`
	ImageUrl string `json:"imageUrl"`
}

type Teams struct {
	Id       int    `json:"id"`
	Label    string `json:"label"`
	ImageUrl string `json:"imageUrl"`
}

type Region struct {
	Id    int    `json:"id"`
	Label string `json:"label"`
}

type Leagues struct {
	Id     int     `json:"id"`
	Label  string  `json:"label"`
	Region Region  `json:"region"`
	Gender Gender  `json:"gender"`
	Teams  []Teams `json:"teams"`
}

type LeagueNationData struct {
	Gender      []Gender      `json:"gender"`
	Nationality []Nationality `json:"nationality"`
	Leagues     []Leagues     `json:"leagues"`
}

func main() {
	godotenv.Load("../../.env")

	// Fetch data from EAFCp
	data := fetchData()

	// save nation images to local
	downloadNationImages(data.Nationality)

	// save team images to local
	downloadTeamsImages(data.Leagues)

	// Save images to s3
	// uploadNationImages()
	uploadTeamImages()

	// Rename image paths
	updateImageNames(data)

	// Insert nations data to mongo
	insertNationData(data.Nationality)
	// Insert league data to mongo
	insertLeagueData(data.Leagues)
	// Insert team data to mongo
	insertTeamData(data.Leagues)

	// Save data to file
	saveDataToFile(data)

	fmt.Println("finished")
}

func fetchData() LeagueNationData {
	// Fetch data from EAFC endpoint
	resp, err := http.Get("https://drop-api.ea.com/rating/fc-24/filters?locale=en")
	if err != nil {
		fmt.Println("No response from request")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body) // response body is []byte

	var result LeagueNationData
	if err := json.Unmarshal(body, &result); err != nil { // Parse []byte to the go struct pointer
		fmt.Println("Can not unmarshal JSON")
	}

	// Filter female leagues from dataset
	var leagueData []Leagues
	for i := range result.Leagues {
		if result.Leagues[i].Gender.Id == 0 {
			leagueData = append(leagueData, result.Leagues[i])
		}
	}
	result.Leagues = leagueData

	return result
}

func saveDataToFile(data LeagueNationData) error {
	content, err := json.Marshal(data)
	if err != nil {
		fmt.Println(err)
	}

	err = os.WriteFile("../../assets/data.json", content, 0644)
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

func downloadNationImages(nations []Nationality) error {

	// Create a channel to track status of image upload
	status := make(chan error)

	// Launch a new Goroutine for each image upload
	for i := range nations {

		go func(nation Nationality) error {
			err := downloadFile.DownloadFile("../../assets/nations/"+strconv.Itoa(nation.Id)+".png", nation.ImageUrl)
			if err != nil {
				fmt.Println("Error downloading file: ", err)
				return nil
			}

			status <- err

			return nil
		}(nations[i])
	}

	// Wait for all uploads to complete and collect status
	for range nations {
		err := <-status
		if err != nil {
			return err
		}
	}

	return nil
}

func downloadTeamsImages(leagues []Leagues) error {

	var wg sync.WaitGroup

	for i := range leagues {
		for v := range leagues[i].Teams {
			wg.Add(1)
			go func(team Teams) error {
				err := downloadFile.DownloadFile("../../assets/teams/"+strconv.Itoa(team.Id)+".png", team.ImageUrl)

				if err != nil {
					fmt.Println("Error downloading file: ", err)
					return nil
				}

				defer wg.Done()

				return nil

			}(leagues[i].Teams[v])
		}
	}

	wg.Wait()

	return nil
}

func updateImageNames(data LeagueNationData) LeagueNationData {

	// Update nation data to use correct url
	for i := range data.Nationality {
		data.Nationality[i].ImageUrl = "https://cdn.lineup-builder.co.uk/nations/24/" + strconv.Itoa(data.Nationality[i].Id) + ".png"
	}

	// Update team data to use correct url
	for i := range data.Leagues {
		for v := range data.Leagues[i].Teams {
			data.Leagues[i].Teams[v].ImageUrl = "https://cdn.lineup-builder.co.uk/clubs/24/" + strconv.Itoa(data.Leagues[i].Teams[v].Id) + ".png"
		}
	}

	return data
}

func uploadNationImages() error {
	nationFiles, err := os.ReadDir("../../assets/nations")
	if err != nil {
		log.Fatal(err)
	}

	var filePaths []string
	for _, file := range nationFiles {
		filePaths = append(filePaths, "../../assets/nations/"+file.Name())
	}

	s3.UploadImagesToS3(os.Getenv("BUCKET_NAME"), filePaths, "nations/24/")
	return nil
}

func uploadTeamImages() error {
	teamFiles, err := os.ReadDir("../../assets/teams")
	if err != nil {
		log.Fatal(err)
	}

	var filePaths []string
	for _, file := range teamFiles {
		filePaths = append(filePaths, "../../assets/teams/"+file.Name())
	}

	s3.UploadImagesToS3(os.Getenv("BUCKET_NAME"), filePaths, "clubs/24/")

	return nil
}

func insertNationData(nations []Nationality) error {
	type Nation struct {
		Id     int    `json:"id"`
		Label  string `json:"label"`
		ImgSrc string `json:"img_src"`
	}

	supabase := supa.CreateClient(os.Getenv("SUPABASE_URL"), os.Getenv("SUPABASE_KEY"))

	for _, nation := range nations {
		row := Nation{
			Id:     nation.Id,
			Label:  nation.Label,
			ImgSrc: nation.ImageUrl,
		}
		var results []Nationality
		err := supabase.DB.From("nations").Upsert(row).Execute(&results)
		if err != nil {
			panic(err)
		}
		fmt.Println(results)
	}

	return nil
}

func insertLeagueData(leagues []Leagues) error {
	type League struct {
		Id       int    `json:"id"`
		Label    string `json:"label"`
		NationId int    `json:"nation_id"`
	}

	supabase := supa.CreateClient(os.Getenv("SUPABASE_URL"), os.Getenv("SUPABASE_KEY"))

	for _, league := range leagues {
		row := League{
			Id:       league.Id,
			Label:    league.Label,
			NationId: league.Region.Id,
		}
		var results []Leagues
		err := supabase.DB.From("leagues").Upsert(row).Execute(&results)
		if err != nil {
			fmt.Println(league)
			panic(err)
		}
		fmt.Println(results)
	}

	return nil
}

func insertTeamData(leagues []Leagues) error {
	type Team struct {
		Id       int    `json:"id"`
		Label    string `json:"label"`
		ImgSrc   string `json:"img_src"`
		NationId int    `json:"nation_id"`
		LeagueId int    `json:"league_id"`
	}

	supabase := supa.CreateClient(os.Getenv("SUPABASE_URL"), os.Getenv("SUPABASE_KEY"))

	for _, league := range leagues {
		for _, team := range league.Teams {
			row := Team{
				Id:       team.Id,
				Label:    team.Label,
				ImgSrc:   team.ImageUrl,
				NationId: league.Region.Id,
				LeagueId: league.Id,
			}
			var results []Team
			err := supabase.DB.From("clubs").Upsert(row).Execute(&results)
			if err != nil {
				fmt.Println(league)
				panic(err)
			}
			fmt.Println(results)
		}

	}
	return nil
}
