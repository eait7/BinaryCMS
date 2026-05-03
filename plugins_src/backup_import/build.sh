#!/bin/bash
go build -o backup_import
mkdir -p ../../plugins
mv backup_import ../../plugins/
echo "Plugin compiled and moved to plugins/backup_import"
