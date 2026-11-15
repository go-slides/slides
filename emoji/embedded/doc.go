// Package embedded registers, with package emoji, the provider go-slides ships:
// the Twemoji graphics of github.com/go-icons/twemoji, about 2 MB of data that
// barely compresses. Importing it for its effect is what the deck package does
// unless it is built with the tag slides_noemoji, which excludes this package's
// code so that none of that data is linked.
package embedded
