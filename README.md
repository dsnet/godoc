# GoDoc HTML Renderer #

## Introduction ##

This module provides the `godoc` tool that renders HTML for Go documentation.
The purpose of this tool is to experiment with proposed GoDoc features.

## Installation ##

The `godoc` tool can be installed by running:
```
go install github.com/dsnet/godoc@latest
```

## Usage ##

Navigate to within a module that you would like rendered and invoke the `godoc` tool.
The tool will serve Go documentation for all packages (including transitively reachable packages) within that module.
This tool only works with Go modules.

The `godoc` tool can be run in one of two modes:

1.  **Serve mode**: In serve mode (the default), `godoc` starts up an HTTP server
    that serves webpages of Go documentation. When the server starts,
    it prints the URL for the current package or module.

    Example usage:
    ```
    $ cd $PROTOBUF_MODULE  # or any other module directory
    
    $ godoc -serve=0.0.0.0:8080
    http://0.0.0.0:8080/google.golang.org/protobuf
    ```

2. **Archive mode**: In archive mode (which is specified using the "-archive" flag),
    `godoc` emits a TAR archive of statically generated HTML files.

    Example usage:
    ```
    $ cd $PROTOBUF_MODULE  # or any other module directory
    
    $ OUTPUT_DIRECTORY=out
    $ mkdir $OUTPUT_DIRECTORY
    $ godoc -archive=- | tar -x --directory $OUTPUT_DIRECTORY
    main.go:117: rendering ""
    main.go:117: rendering "archive"
    main.go:117: rendering "archive/tar"
    main.go:117: rendering "archive/zip"
    main.go:117: rendering "bufio"
    main.go:117: rendering "builtin"
    main.go:117: rendering "bytes"
    ...

    $ cd $OUTPUT_DIRECTORY
    $ python -m SimpleHTTPServer
    Serving HTTP on 0.0.0.0 port 8000 ...
    ```

    The example above emits a TAR archive to stdout,
    which we immediately extract into some output directory.
    Afterwards, we change the working directory that output directory and
    use Python's SimpleHTTPServer module to serve the statically generated files.