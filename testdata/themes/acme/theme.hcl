# ACME example theme package. Pack it with:
#   slides theme pack testdata/themes/acme
theme {
  name        = "acme"
  version     = "1.0.0"
  description = "Example theme: a base theme, one font, a logo and a background"
  authors     = ["go-slides contributors"]
  license     = "MIT"
  homepage    = "https://go-slides.github.io"
  extends     = "default"
  min_slides  = "0.17.0"
}

css "theme.css" {}

font "Roboto Mono" {
  weight  = 400
  style   = "normal"
  file    = "fonts/RobotoMono-Regular.ttf"
  license = "OFL-1.1"
}

asset "logo" {
  file = "assets/logo.svg"
}

asset "background" {
  file = "assets/bg.png"
}

kind "acme-title" {
  class    = "lead"
  paginate = "false"
}
