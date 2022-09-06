#!/bin/bash
# ===================================================================================
# Init script which sets up certificates in NSSDB for FIPS.
# ===================================================================================

function initKeystores() {
  NSSDB=/etc/pki/nssdb
  KEYSTORE_SECRET=""
  WORKING_DIR=""

  ARGS=()

  while [ $# -gt 0 ]; do
    case $1 in
      -d|--database)
        NSSDB="$2"
        shift 2
        ;;
      -p|--password)
        KEYSTORE_SECRET="$2"
        shift 2
        ;;
      -w|--working-dir)
        WORKING_DIR="$2"
        shift 2
        ;;
      -*)
        echo "Unknown option $1"
        exit 1
        ;;
      *)
        ARGS+=("$1")
        shift
        ;;
    esac
  done

  set -- "${ARGS[@]}"


  if [ "$#" -eq 0 ]; then
    echo "Usage: $0 [-d nssdb] path [password]"
    exit 1
  fi

  KEYSTORE_PATH=${1%/}

  if [ ! -d "$NSSDB" ]; then
    echo "Directory $NSSDB does not exist"
    exit 1
  fi

  if [ ! -e "$NSSDB/pkcs11.txt" ]; then
    echo "Directory $NSSDB does not appear to be a NSS database"
    exit 1
  fi

  if [ "${WORKING_DIR}x" == "x" ]; then
    WORKING_DIR=$KEYSTORE_PATH
  fi

  PEM_FILES=$(ls -1 "$KEYSTORE_PATH"/*.pem 2>/dev/null | wc -l)
  CERTIFICATES=$(ls -1 "$KEYSTORE_PATH"/*.crt 2>/dev/null | wc -l)

  if [ "$PEM_FILES" != 0 ]; then
    for PEM in $KEYSTORE_PATH/*.pem; do
      NAME=$(basename "$PEM" .pem)
      echo "Converting $NAME.pem to $NAME.p12"
      openssl pkcs12 -export -out "$WORKING_DIR/$NAME.p12" -in "$PEM" -name "$NAME" -password "pass:$KEYSTORE_SECRET"
    done
  fi

  if [ "$CERTIFICATES" != 0 ]; then
    for CRT in $KEYSTORE_PATH/*.crt; do
      NAME=$(basename "$CRT" .crt)
      echo "Converting $NAME.crt/$NAME.key to $NAME.p12"
      openssl pkcs12 -export -out "$WORKING_DIR/$NAME.p12" -inkey "$KEYSTORE_PATH/$NAME.key" -in "$CRT" -name "$NAME" -password "pass:$KEYSTORE_SECRET"
    done
  fi

  if [ "$PEM_FILES" == 0 ] && [ "$CERTIFICATES" == 0 ] && [ "${KEYSTORE_SECRET}x" == "x" ]; then
    echo "Importing PKCS#12 certificates requires passing the password"
    exit 1
  fi

  for P12 in $KEYSTORE_PATH/*.p12 $WORKING_DIR/*.p12; do
    if [ -f "$P12" ]; then
      echo "Importing $P12"
      pk12util -l "$P12" -W "$KEYSTORE_SECRET"
      if ! pk12util -v -i "$P12" -d "$NSSDB" -W "$KEYSTORE_SECRET" -K ""; then
        echo "An error occurred. Aborting."
        exit 1
      fi
    fi
  done

  certutil -L -d "$NSSDB"
}

set -e

{{ range . }}
initKeystores {{ if .Secret }}-p {{ .Secret }}{{ end }} -w /tmp {{ .Path }}
{{ end }}
