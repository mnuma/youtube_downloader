package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"path/filepath"

	"github.com/KeluDiao/gotube/api"
	"github.com/gin-gonic/gin"
	"github.com/nu7hatch/gouuid"
	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	storage "google.golang.org/api/storage/v1"
	"github.com/k0kubun/pp"
)

const (
	endpoint string = "/v1/youtube/"
	scope           = storage.DevstorageFullControlScope
	tmp             = "./tmp"

	quality   = "medium"
	extension = "video/mp4"

	bucketName = "bucket-pre-hakone"
	sec = "1"
)

var (
	acls []*storage.ObjectAccessControl

	uniqdir, _ = uuid.NewV4()
)

type (
	Response struct {
		Url        string                `json:"url"`
		Thumbnails *[]ThumbnailsResponse `json:"thumbnails"`
	}

	ThumbnailsResponse struct {
		Sec       int    `json:"sec"`
		Thumbnail string `json:"thumbnail"`
	}
)

func init() {
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "./credectial.json")

	objectAcl := &storage.ObjectAccessControl{
		Bucket: bucketName,
		Entity: "allUsers",
		Role:   "OWNER",
	}
	acls = append(acls, objectAcl)
}

func main() {
	router := gin.Default()
	router.GET("/health", health)

	v1 := router.Group(endpoint)
	{
		v1.GET("/storybords", storybords)
	}
	router.Run()
}

// health check
func health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "OK",
	})
}

func storybords(c *gin.Context) {

	url := c.Query("url")

	var videoList gotube.VideoList
	var err error

	// getting youtube video.
	videoList, err = gotube.GetVideoListFromUrl(url)
	videoList.Filter(quality, extension)
	filename := videoList.Videos[0].Title

	// youtube video download.
	videoList.Download(tmp, quality, extension)
	if err != nil {
		log.Fatal("%v", err)
	}

	// create thumbnails. should ffmpeg command install.
	// exexc command. ffmpeg  -i ./TEST\ VIDEOvideo.mp4 -r 1 -f image2 frame%d.jpg
	path := tmp + "/" + filename + "video" + ".mp4"
	_, err = exec.Command("ffmpeg", "-i", path, "-r", sec, "-f", "image2", tmp+"/%d.jpg").Output()
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer clean(tmp)

	// getting local jpg files.
	var jpgs []string
	files, _ := ioutil.ReadDir(tmp)
	for _, file := range files {
		if strings.Contains(file.Name(), "jpg") {
			jpgs = append(jpgs, file.Name())
		}
	}

	// upload to google cloud storage.
	client, err := google.DefaultClient(context.Background(), scope)
	if err != nil {
		log.Fatalf("Unable to get default client: %v", err)
	}

	service, err := storage.New(client)
	if err != nil {
		log.Fatalf("Unable to create storage service: %v", err)
	}

	var thumbs []ThumbnailsResponse
	for index, thumbnail := range jpgs {

		object := &storage.Object{
			Acl: acls,
			// TODO: uniqdirではなくファイル名をハッシュ化してパス作る
			Name: fmt.Sprintf("%s", fmt.Sprint(uniqdir)) + "/" + thumbnail,
		}
		file, err := os.Open(tmp + "/" + thumbnail)
		if err != nil {
			log.Fatalf("%v", err)
		}

		// upload google cloud storage.
		if res, err := service.Objects.Insert(bucketName, object).Media(file).Do(); err == nil {
			log.Printf("Created object %v at location %v\n\n", res.Name, res.SelfLink)
		} else {
			log.Fatalf("%v", err)
		}

		responseThumbnails := &ThumbnailsResponse{
			Sec: index + 1,
			// https://storage.googleapis.com/{bucketName}/49d3d4c3-3167-41ae-61e2-57624a02363a/1.jpg
			Thumbnail: "https://storage.googleapis.com/" + bucketName + "/" + fmt.Sprintf("%s", fmt.Sprint(uniqdir)) + "/" + thumbnail,
		}
		thumbs = append(thumbs, *responseThumbnails)
	}

	pp.Print(thumbs)

	response := &Response{
		Url:        url,
		Thumbnails: &thumbs,
	}
	c.JSON(http.StatusOK, response)
}

func clean(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()

	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		pp.Print(name)
		if err != nil {
			return err
		}
	}
	return nil
}