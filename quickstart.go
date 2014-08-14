package main

import (
	"code.google.com/p/goauth2/oauth"
	"code.google.com/p/google-api-go-client/drive/v2"

	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
)

// Settings for authorization.
var config = &oauth.Config{
	ClientId:     "YOUR_CLIENT_ID",
	ClientSecret: "YOUR_CLIENT_SECRET",
	Scope:        drive.DriveScope,
	RedirectURL:  "urn:ietf:wg:oauth:2.0:oob",
	AuthURL:      "https://accounts.google.com/o/oauth2/auth",
	TokenURL:     "https://accounts.google.com/o/oauth2/token",
}

var flag_token_cache_file = flag.String("credentials", "auth.json", "Local file to keep authorization token.")

var flag_local_file = flag.String("local_file", "", "Local file to be uploaded.")
var flag_local_dir = flag.String("local_dir", "", "Local directory to scan for files.")
var flag_file_pattern = flag.String("file_pattern", "", "A file name pattern to match.")

var flag_drive_file = flag.String("drive_file", "", "A Google Drive file path.")
var flag_drive_dir = flag.String("drive_dir", "", "A Google Drive directory path.")

func Usage() {
	// TODO describe the usage.
	fmt.Println(
		"The paths:")
}

func GetNewToken() (*oauth.Token, *oauth.Transport) {
	// Generate a URL to visit for authorization.
	authUrl := config.AuthCodeURL("state")
	log.Printf("Go to the following link in your browser: %v\n", authUrl)
	t := &oauth.Transport{
		Config:    config,
		Transport: http.DefaultTransport,
	}

	// Read the code, and exchange it for a token.
	log.Printf("Enter verification code: ")
	var code string
	fmt.Scanln(&code)
	token, err := t.Exchange(code)
	if err != nil {
		log.Fatalf("An error occurred exchanging the code: %v\n", err)
	}
	return token, t
}

func InsertFile(svc *drive.Service, localPath, driveParentId, driveFileName string) {
	// Read the file data that we are going to upload.
	m, err := os.Open(localPath)
	defer m.Close()
	if err != nil {
		log.Fatalf("An error occurred reading the document: %v\n", err)
	}

	// Define the metadata for the file we are going to create.
	f := &drive.File{
		Title:       driveFileName,
		Description: "Google Drive uploader.",
	}
	if driveParentId != "" {
		p := &drive.ParentReference{Id: driveParentId}
		f.Parents = []*drive.ParentReference{p}
	}

	// Make the API request to upload metadata and file data.
	r, err := svc.Files.Insert(f).Media(m).Do()
	if err != nil {
		log.Fatalf("An error occurred uploading the document: %v\n", err)
	}
	log.Printf("Created: ID=%v, Title=%v\n", r.Id, r.Title)
}

func SearchForSubdir(svc *drive.Service, parentFolderId, title string) (*drive.ChildReference, error) {
	only_folders := "mimeType = 'application/vnd.google-apps.folder'"
	query := fmt.Sprintf("%s and title = '%s'", only_folders, title)

	var fs []*drive.ChildReference
	pageToken := ""
	num_found_dirs := 0
	for {
		q := svc.Children.List(parentFolderId)
		// If we have a pageToken set, apply it to the query
		if pageToken != "" {
			q = q.PageToken(pageToken)
		}
		r, err := q.Q(query).Do()
		if err != nil {
			log.Fatalf("Error when searching for dir: %v\n", err)
			return nil, err
		}
		num_found_dirs += len(r.Items)
		fs = append(fs, r.Items...)
		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
	}
	log.Printf("Found: %d matching dirs", num_found_dirs)
	if len(fs) > 1 {
		return nil, fmt.Errorf("Ambiguous subdirectory name: %s.", title)
	} else if len(fs) == 0 {
		return nil, nil
	}
	return fs[0], nil
}

func MakeSubdir(svc *drive.Service, parentFolderId, title string) (*drive.File, error) {
	// Define the metadata for the file we are going to create.
	f := &drive.File{
		Title:    title,
		MimeType: "application/vnd.google-apps.folder",
	}
	if parentFolderId != "" {
		p := &drive.ParentReference{Id: parentFolderId}
		f.Parents = []*drive.ParentReference{p}
	}
	// Make the API request to upload metadata and file data.
	r, err := svc.Files.Insert(f).Do()
	if err != nil {
		log.Fatalf("An error occurred creating subdir: %v\n", err)
	}
	log.Printf("Created: ID=%v, Title=%v\n", r.Id, r.Title)
	return r, err
}

func FindOrCreatePath(svc *drive.Service, parentFolderId string, dirs []string) (string, error) {
	if len(dirs) == 0 {
		return "", fmt.Errorf("FindOrCreatePath cannot search for empty path")
	}
	c, err := SearchForSubdir(svc, parentFolderId, dirs[0])
	if err != nil {
		return "", err
	}

	var folderId string
	if c != nil {
		folderId = c.Id
	} else {
		f, err := MakeSubdir(svc, parentFolderId, dirs[0])
		if err != nil {
			return "", err
		}
		folderId = f.Id
	}
	if len(dirs) > 1 {
		return FindOrCreatePath(svc, folderId, dirs[1:])
	}
	return folderId, nil
}

func SplitPath(dirPath string) []string {
	if len(dirPath) == 0 {
		return []string{}
	}
	s := []string{}
	var l string
	for dirPath != "/" && dirPath != "." {
		// fmt.Println(dirPath)
		dirPath, l = path.Split(dirPath)
		s = append(s, l)
		dirPath = path.Dir(dirPath)
	}
	ret := make([]string, len(s))
	for idx, v := range s {
		ret[len(s)-1-idx] = v
	}
	return ret
}

func BloodyTest(svc *drive.Service) {
	// InsertFile(svc)

	subdir, _ := SearchForSubdir(svc, "root", "Fotos")
	if subdir == nil {
		log.Printf("Cannot find Fotos subdir")
		return
	}

	subdir, _ = SearchForSubdir(svc, subdir.Id, "2014 Portugalia")
	if subdir == nil {
		log.Printf("Cannot find Portugalia subdir")
		return
	}
}

// type WalkFunc func(path string, info os.FileInfo, err error) error
func CreateWalkFunc(reFilter regexp.Regexp, recursive bool, actFunc filepath.WalkFunc) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		//fmt.Println(path)
		if info.IsDir() {
			//if !recursive {
			//	return filepath.SkipDir
			// }
			return nil
		}
		// upload
		if !reFilter.MatchString(path) {
			return nil
		}
		return actFunc(path, info, err)
	}
}

func CreateCheckIfNotExistFunc(fileList []string, actFunc filepath.WalkFunc) filepath.WalkFunc {
	dct := make(map[string]int)
	for idx, name := range fileList {
		dct[name] = idx
	}
	return func(path string, info os.FileInfo, err error) error {
		if _, ok := dct[info.Name()]; ok {
			log.Printf("Skip existing name: %s", info.Name())
			return nil
		}
		return actFunc(path, info, err)
	}
}

func CreatePrintFunc() filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		log.Printf("Path: %s", path)
		return nil
	}
}

func CreateUploadFunc(svc *drive.Service, parentFolderId string) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		log.Printf("Path to upload %s", path)
		return nil
	}
}

// Uploads a file to Google Drive
func main() {
	flag.Parse()

	var cache oauth.Cache = oauth.CacheFile(*flag_token_cache_file)
	token, err := cache.Token()
	var t *oauth.Transport
	if err != nil {
		log.Printf("Need a new token. Cannot load old one.")
		token, t = GetNewToken()
		cache.PutToken(token)
	} else {
		t = &oauth.Transport{
			Config:    config,
			Token:     token,
			Transport: http.DefaultTransport,
		}
	}

	// Create a new authorized Drive client.
	svc, err := drive.New(t.Client())
	if err != nil {
		log.Fatalf("An error occurred creating Drive client: %v\n", err)
	}

	// Google Drive filename (title)
	var driveFileName string
	if len(*flag_drive_file) > 0 {
		driveFileName = *flag_drive_file
	} else {
		driveFileName = path.Base(*flag_local_file)
	}

	// Google Drive directory (folder id)
	var folderId string = "root"
	if dirpathList := SplitPath(*flag_drive_dir); len(dirpathList) > 0 {
		folderId, err = FindOrCreatePath(svc, "root", dirpathList)
		if err != nil {
			log.Panicf("Error when trying to find a Google Drive path: %v\n", err)
		}
	}
	log.Printf("local path: %s ; folder id: %s ; drive file name: %s\n", *flag_local_file, folderId, driveFileName)

	if len(*flag_local_dir) > 0 {
		re, err := regexp.Compile(".(JPG|jpg)")
		if err != nil {
			log.Panic("RegExp not compiling.")
		}
		a := []string{"poland-ball.jpg"}
		f := CreateWalkFunc(*re, false, CreateCheckIfNotExistFunc(a, CreatePrintFunc()))
		filepath.Walk(*flag_local_dir, f)
	}
	//InsertFile(svc, *flag_local_file, folderId, driveFileName)

	// folderId, _ := FindOrCreatePath(svc, "root", []string{"test0", "test1", "test2"})
	// log.Printf("Dir ID=%v\n", folderId)
}
