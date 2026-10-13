module github.com/ensamuel7/dex

go 1.25.0

// v0.6.4 was tagged on a commit that does not compile: it was made while an
// unresolved `git stash pop` sat in the tree, so seven files still carried
// conflict markers. The tag is already published and the module proxy treats a
// version as immutable, so it is retracted here rather than moved. Use v0.6.5.
retract v0.6.4

require (
	github.com/fsnotify/fsnotify v1.10.1
	golang.org/x/term v0.45.0
)

require golang.org/x/sys v0.47.0 // indirect
