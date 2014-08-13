google_drive_upload
===================

Smart Google Drive upload app

Uploads a directory (optionally recursively) to a Google Drive directory. 
Skips the files with same name.

This app was created after my vacation in Portugal when I've come with over 6.5k photos and the web app at drive.google.com was hanging after few hundred uploads.

Example:

    go run main.go --recursive --flatten --local_dir=/home/orian/photos/2014/portugal --drive_dir=/Photos/2014_Portugal

Where:  
`--recursive` says to walk the local_dir recursivly  
`--flaten` means to upload all files into one directory instead of whole tree structure  
`--local_dir` is a local directory to scan for files  
`--drive_dir` is a destination on Google Drive  
