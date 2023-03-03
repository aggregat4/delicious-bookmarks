# Simple Online Bookmarks Storage

## Launching

There is only one prerequisite to launching the program. An environment variable must be present called `BOOKMARKS_SESSION_COOKIE_SECRET_KEY` that contains the secret key for encrypting the session cookie for the application.

## Bookmarklet

You can call the `/addbookmark` page using a bookmarklet in the browser to quickly allow capturing new bookmarks. The code can look something like this:

```js
javascript:void(window.open(`http://localhost:1323/addbookmark?description=${encodeURIComponent(document.querySelector('meta[name="description"]')?.content  ?? document.querySelector('meta[name="twitter:description"]')?.content ?? "")}&title=${encodeURIComponent(document.title)}&url=${encodeURIComponent(location.href)}`,'Save Bookmark', 'width=700,height=500,left=0,top=0,resizable=yes,toolbar=no,location=no,scrollbars=yes,status=no,menubar=no'));
```

Note the usage of query parameters to prefill a bunch of fields.

## TODO

- `addbookmark` should check whether the URL is already present and if it is, don't overwrite existing parameters but allow user to edit if needed, then UPSERT
- delete bookmark in the bookmark list
- import command line mode: takes a path to a bookmarks.html and imports all those entries into a user's bookmarks, adding to all existing ones (need to detect duplicates and ignore?)
- edit bookmark in the bookmark list  (inline editing form?) (low prio, but since I need a separate create bookmark page for the bookmarklet/extension this may also be the way (see for example when adding a bookmark that is already added, it becomes an edit))
