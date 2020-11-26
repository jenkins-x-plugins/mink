#!/usr/bin/env bash

# a sample digest for testing
digest="sha256:8e65ec4b80519d869e8d600fdf262c6e8cd3f6c7e8382406d9cb039f352a69bc"

echo "fake-kaniko building digest $digest"

for arg in "$@"
do
  if [[ $arg == "--digest-file="* ]] ;
  then
      df=${arg#"--digest-file="}
      echo "writing to digest file: $df"
      echo "$digest" > $df
  fi
done



