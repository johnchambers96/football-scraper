package s3

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

func UploadImagesToS3(bucketName string, imagePaths []string, prefix string) error {
	for _, imagePath := range imagePaths {
		err := UploadImage(bucketName, imagePath, prefix)
		if err != nil {
			panic(err)
		}
	}

	return nil
}

func UploadImage(bucketName string, imagePath string, prefix string) error {
	// Create a new session with default session credentials
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(os.Getenv("BUCKET_REGION")),
		Credentials: credentials.NewStaticCredentials(
			os.Getenv("ACCESS_KEY"),
			os.Getenv("SECRET_KEY"),
			"", // a token will be created when the session it's used.
		),
	},
	)

	if err != nil {
		return err
	}

	// Open the image file
	file, err := os.Open(imagePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Get the file size and content type
	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}
	fileSize := fileInfo.Size()

	// Create a new S3 service client
	svc := s3.New(sess)

	// Configure the S3 object input parameters
	input := &s3.PutObjectInput{
		Body:          file,
		Bucket:        aws.String(bucketName),
		Key:           aws.String(prefix + filepath.Base(imagePath)),
		ContentType:   aws.String("image/png"),
		ContentLength: aws.Int64(fileSize),
	}

	// Upload the image to S3
	_, err = svc.PutObject(input)
	if err != nil {
		fmt.Println(err)
		return err
	}

	return nil
}
