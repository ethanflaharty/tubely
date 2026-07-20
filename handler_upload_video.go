package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	_ = http.MaxBytesReader(w, r.Body, 1<<30)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error getting video from database", err)
		return
	}
	if userID != video.UserID {
		respondWithError(w, http.StatusUnauthorized, "User does not own the video", err)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error getting video file and header", err)
		return
	}

	defer file.Close()

	rawContentType := header.Header.Get("Content-Type")

	mediaType, _, err := mime.ParseMediaType(rawContentType)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error parsing media type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Incompatible file type", err)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating temporary file", err)
		return
	}

	defer tempFile.Close()

	defer os.Remove("tubely-upload.mp4")

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying file contents", err)
		return
	}

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error reseting file pointer", err)
		return
	}

	extensions, err := mime.ExtensionsByType(mediaType)
	if err != nil {
		if len(extensions) == 0 {
			respondWithError(w, http.StatusBadRequest, "Extensions not found", err)
			return
		}
		respondWithError(w, http.StatusInternalServerError, "Error getting extensions", err)
		return
	}

	random := make([]byte, 32)
	_, err = rand.Read(random)
	if err != nil {
		panic("failed to generate random bytes")
	}
	randomString := base64.RawURLEncoding.EncodeToString(random)

	fileName := randomString + extensions[0]

	bucket := "tubely-20101003"
	cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &bucket,
		Key:         &fileName,
		Body:        tempFile,
		ContentType: &mediaType,
	})

	url := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v", bucket, cfg.s3Region, fileName)
	video.VideoURL = &url

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error updating video in database", err)
		return
	}
}
