#!/bin/bash

if [[ -z $2 ]]; then
  cat << EOF
Usage:"
  upload.sh {server_url} {path}

Example:
  upload.sh http://localhost sample
  
EOF
  exit 1
fi

join_array() {
  local d=$1
  local -n ary=$2
  tailary=("${ary[@]:1}")
  printf %s "${ary[0]}" "${tailary[@]/#/$d}"
}

upload_files() {
  local -n rtags=$1
  local -n rfiles=$2
  local tags=$(join_array " " rtags)
  echo using tags=\"${tags}\" to upload files:
  join_array $'\n' rfiles
  echo

  args=()
  for file in "${files[@]}"; do
    args+=('-F' "files=@${file}")
  done
  curl --insecure --progress-bar -w "\nTime taken: %{time_total} seconds\n" -X POST ${url}/api/files -H "Content-Type: multipart/form-data" -F tags="${tags}" ${args[@]} 
  echo
}

upload_dir() {
  local -n rtags=$1
  local dir="$2"
  local tags=("${rtags[@]}" $(basename $dir))
  echo "processing $dir with tags=$(join_array "," tags)"
  
  local files=($(find "$dir" -maxdepth 1 -type f))
  if [ $? -eq 0 ]; then
    upload_files tags files
  fi
  upload_subdirs tags "$dir"
}

upload_subdirs() {
  local -n rtags=$1
  local ltags=("${rtags[@]}")
  dirs=($(find "$2" -mindepth 1 -maxdepth 1 -type d))
  for dir in "${dirs[@]}"; do
    upload_dir ltags $dir
  done
}

url=$1
dir=$2
tags=()
upload_dir tags "$dir"
