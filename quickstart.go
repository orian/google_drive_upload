package main

import (
	"code.google.com/p/goauth2/oauth"
	"code.google.com/p/google-api-go-client/drive/v2"

	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"time"
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

var flag_log_file = flag.String("log_file", "", "A log file.")

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
	log.Printf("Created: ID=%v, Title=%v, Size=%dB (%dMB)\n", r.Id, r.Title, r.FileSize, (r.FileSize+512)/1024/1024)
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

func AllFilesInDir2(svc *drive.Service, parentFolderId string) ([]*drive.ChildReference, error) {
	var fs []*drive.ChildReference
	pageToken := ""
	for {
		q := svc.Children.List(parentFolderId)
		// If we have a pageToken set, apply it to the query
		if pageToken != "" {
			q = q.PageToken(pageToken)
		}
		r, err := q.Do()
		if err != nil {
			log.Fatalf("Error when searching for dir: %v\n", err)
			return nil, err
		}
		fs = append(fs, r.Items...)
		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
	}

	// fetch metadata for each fs
	return fs, nil
}

func GetMetadatas(svc *drive.Service, children []*drive.ChildReference) ([]*drive.File, error) {
	var fs []*drive.File
	for _, child := range children {
		f, err := svc.Files.Get(child.Id).Do()
		if err != nil {
			log.Printf("An error occurred: %v\n", err)
			continue
		}
		fs = append(fs, f)
	}
	return fs, nil
}

// AllFiles fetches and displays all files
func AllFilesInDir(d *drive.Service, parentFolderId string) ([]*drive.File, error) {
	var fs []*drive.File
	pageToken := ""
	for {
		q := d.Files.List()
		// If we have a pageToken set, apply it to the query
		if pageToken != "" {
			q = q.PageToken(pageToken)
		}
		if parentFolderId != "" {
			q = q.Q(fmt.Sprintf("'%s' in parents", parentFolderId))
		}
		r, err := q.Do()
		if err != nil {
			fmt.Printf("An error occurred: %v\n", err)
			return fs, err
		}
		fs = append(fs, r.Items...)
		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return fs, nil
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
		InsertFile(svc, path, parentFolderId, info.Name())
		return nil
	}
}

func BenchmarkGetAllFiles(svc *drive.Service) {
	photosDir := "0BzlEOKvvsS8la2hxSnA0cVRnOW8"
	t0 := time.Now()
	_, err := AllFilesInDir(svc, photosDir)
	t1 := time.Now()
	if err != nil {
		log.Printf("Error: %v", err)
	}
	log.Printf("AllFilesInDir(): %v", t1.Sub(t0))
	t2 := time.Now()
	children, err := AllFilesInDir2(svc, photosDir)
	_, err = GetMetadatas(svc, children)
	t3 := time.Now()
	if err != nil {
		log.Printf("Error: %v", err)
	}
	log.Printf("AllFilesInDir2(): %v", t3.Sub(t2))

	// Result for 273 elements in directory
	// 2014/08/14 23:18:40 AllFilesInDir(): 6.331292724s
	// 2014/08/14 23:20:14 AllFilesInDir2(): 1m33.916649055s
}

func FileNames(files []*drive.File) []string {
	var fs []string
	for _, f := range files {
		fs = append(fs, f.Title)
	}
	return fs
}

func InitLog() {
	if len(*flag_log_file) == 0 {
		return
	}
	log_file, err := os.OpenFile(*flag_log_file, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening log file: %v", err)
	}
	out := io.MultiWriter(os.Stdout, log_file)
	log.SetOutput(out)
}

// Uploads a file to Google Drive
func main() {
	flag.Parse()
	InitLog()

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

	//BenchmarkGetAllFiles(svc)
	//return

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
		// get already existing files
		files, err := AllFilesInDir(svc, folderId)
		if err != nil {
			log.Fatalf("An error occurred getting files in directory: %v\n", err)
		}
		fileNames := FileNames(files)
		l := len(fileNames)
		fmt.Println(l)
		if l > 10 {
			l = 10
		}
		fmt.Println(fileNames[:l])

		re, err := regexp.Compile(".(JPG|jpg)")
		if err != nil {
			log.Panic("RegExp not compiling.")
		}
		f := CreateWalkFunc(*re, false, CreateCheckIfNotExistFunc(fileNames, CreateUploadFunc(svc, folderId)))
		filepath.Walk(*flag_local_dir, f)
	}
	//InsertFile(svc, *flag_local_file, folderId, driveFileName)

	// folderId, _ := FindOrCreatePath(svc, "root", []string{"test0", "test1", "test2"})
	// log.Printf("Dir ID=%v\n", folderId)
}
