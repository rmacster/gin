# Robot headshots

By default each robot is drawn with an original, generated SVG caricature (a
stylized archetype — rabbit, robot, star, etc. — that evokes the character
without copying any trademarked artwork).

To override a robot with your own (licensed/owned) image, drop a square image
named `<slug>.png` in this folder. If the file is present it replaces the
generated caricature; otherwise the caricature is shown (so everything works
out of the box).

The slug is the character name lower-cased with non-alphanumerics turned into
single hyphens. Files for the built-in cartoon characters:

```
bugs-bunny.png        mickey-mouse.png     daffy-duck.png       donald-duck.png
homer-simpson.png     bart-simpson.png     spongebob.png        scooby-doo.png
fred-flintstone.png   tom-cat.png          jerry-mouse.png      popeye.png
yogi-bear.png         tweety-bird.png      wile-e-coyote.png    road-runner.png
porky-pig.png         pink-panther.png     bender.png           stewie-griffin.png
peter-griffin.png     rick-sanchez.png     morty-smith.png      patrick-star.png
velma.png             shaggy.png           elmer-fudd.png       marvin-martian.png
foghorn-leghorn.png   snoopy.png
```

Images are served from `/robots/<slug>.png`. Square images (e.g. 128×128) look
best; they're shown in a circular 48px frame, cropped to cover.
