#!/bin/bash

printf "** Building linux/386\n"
go-linux-386 build github.com/zerklabs/libsmtp
go-linux-386 install github.com/zerklabs/libsmtp

printf "** Building linux/amd64\n"
go-linux-amd64 build github.com/zerklabs/libsmtp
go-linux-amd64 install github.com/zerklabs/libsmtp

printf "** Building windows/386\n"
go-windows-386 build github.com/zerklabs/libsmtp
go-windows-386 install github.com/zerklabs/libsmtp

printf "** Building windows/amd64\n"
go-windows-amd64 build github.com/zerklabs/libsmtp
go-windows-amd64 install github.com/zerklabs/libsmtp
