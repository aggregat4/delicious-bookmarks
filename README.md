# Simple Online Bookmarks Storage

## Launching

The program will generate a random session key, this means that after each restart user provided cookies are invalid and they need to login again. You can configure a secret key to use by setting the environment variable `BOOKMARKS_SESSION_COOKIE_SECRET_KEY`.

The program expects a `bookmarks.sqlite` sqlite3 database with an appropriate schema to exist. You can initialise a database with the following command:

```
TODO
```

The password for the you provide must be `bcrypt` hashed and salted. You can call the program with the following parameters to create a hashed and salted string:

```
./gobookmarks -passwordtohash 'thisismysecretpassword'
```

## Bookmarklet

You can call the `/addbookmark` page using a bookmarklet in the browser to quickly allow capturing new bookmarks. The code can look something like this (replace the URL with your own instance):

```js
javascript:void(window.open(`http://localhost:1323/addbookmark?description=${encodeURIComponent(document.querySelector('meta[name="description"]')?.content  ?? document.querySelector('meta[name="twitter:description"]')?.content ?? "")}&title=${encodeURIComponent(document.title)}&url=${encodeURIComponent(location.href)}`,'Save Bookmark', 'width=700,height=500,left=0,top=0,resizable=yes,toolbar=no,location=no,scrollbars=yes,status=no,menubar=no'));
```

Note the usage of query parameters to prefill a bunch of fields.

## TODO

- Refactor the code to just use a UUID for the session secret (generated at startup). There is no need to use a configured one :facepalm:
- `addbookmark` should check whether the URL is already present and if it is, don't overwrite existing parameters but allow user to edit if needed, then UPSERT
- delete bookmark in the bookmark list
- import command line mode: takes a path to a bookmarks.html and imports all those entries into a user's bookmarks, adding to all existing ones (need to detect duplicates and ignore?)
- edit bookmark in the bookmark list  (inline editing form?) (low prio, but since I need a separate create bookmark page for the bookmarklet/extension this may also be the way (see for example when adding a bookmark that is already added, it becomes an edit))
