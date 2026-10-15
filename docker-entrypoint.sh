#!/bin/bash

# Deployments that bake a dedicated conf/app.conf (for example the smy test
# image with MySQL settings) set CASDOOR_DB_FROM_CONFIG=true so the SQLite
# fallbacks below do not silently override the packaged configuration.
if [ "${CASDOOR_DB_FROM_CONFIG:-}" != "true" ]; then
if [ -z "${driverName:-}" ]; then
  export driverName=sqlite
fi
if [ -z "${dataSourceName:-}" ]; then
  export dataSourceName="file:casdoor.db?cache=shared"
fi
fi

exec /server
