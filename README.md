# apifs: The API file system

apifs enables users to interact with REST APIs (and any HTTP resource) as a
file system.

# Installation

`go get github.com/gophergala/api-fs/cmd/apifs`

# Usage

To start the apifs service:

`apifs -mountpoint=(mountpoint)`

Once the apifs service is running, make a path that corresponds to a valid URL.
For example:

 apifs -mountpoint=/mnt/apifs &
 mkdir -p /mnt/apifs/golang.org/pkg

Each subdirectory in apifs contains two files, `ctl` and `body`. To execute a
request, write to `ctl` and close the file. Valid `ctl` file formats are
described below.

To read the request response, read the `body` file in the same directory. If
the request was successful, `body` will contain the full body of the response.
Reads from `body` will block until `ctl` has been closed and the request has
finished.

# Control file

The control file `ctl` contains a newline-delimited list of arguments for the
request. Valid forms are as follows:

* method method_type: HTTP request method
* query key [value]: HTTP query parameter with optional value
* header key [value]: HTTP header with optional value
