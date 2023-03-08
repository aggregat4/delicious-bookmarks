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

## Architecture

This is a web application written in go, distributed as a statically linked binary. It stores its data in two tables in a sqlite 3 database: `users` and `bookmarks`.

User authentication is form and password based. Passwords are stored salted and hashed using Bcrypt.

After logging into the application a cookie is set that is symmetrically encrypted and which contains all the required session information (basically the userid).

The web application is completely JavaScript free, not for ideological reasons but it doesn't need any at the moment.

The application supports adding, editing and showing all bookmarks.

The bookmarks collection page is reverse chronologically sorted and contains _all_ kown bookmarks on one page. This has the advantage of giving search for free (just find on the page) and it stops us from having to implement paging. The disadvantage is that the page can get big. The contents are sent gzip encoded and for browsers that support it, the bookmarks are rendered with `content-visibility` set to `auto` and a guesstimated constant intrinsic size. This means that browsers that support it will only render one viewport full of bookmarks at a time. Sadly Firefox does not yet support this feature.

One requirement of the design was that the addbookmark page was easily invokable from a bookmarklet or a browser extension as a shortcut for adding new URLs.

## TODO

- if-modified-since caching
- delete bookmark in the bookmark list
- edit bookmark in the bookmark list (just open in the addbookmarks page is fine)
