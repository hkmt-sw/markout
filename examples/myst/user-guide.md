---
title: Signal Toolkit User Guide
author: Signal Toolkit Team
date: 2026-06-18
---

(top)=
# Signal Toolkit User Guide

```{contents}
```

This page is written in MyST, the Markdown that Sphinx and Jupyter Book
read. It uses directives (blocks that start with a name in braces) and
roles (the same, inside a line of text).

```{toctree}
:caption: In this guide
:maxdepth: 2

Installation <install>
Filtering signals <filtering>
API reference <api>
```

## Installation

```{code-block} sh
pip install signal-toolkit
```

```{versionadded} 2.0
Wheels for Apple silicon. Earlier versions are built from source on those
machines, which needs a C compiler.
```

:::{note}
The toolkit needs Python 3.10 or newer. Press {kbd}`Ctrl` and {kbd}`C` to
stop a script that is still running.
:::

## Filtering a signal

A moving average replaces each sample by the mean of the $n$ samples around
it. For a signal $x$ the filtered signal {math}`y` is

```{math}
:label: moving-average
y_k = \frac{1}{n} \sum_{i=0}^{n-1} x_{k-i}
```

It smooths noise, and it also blurs sharp edges: the larger $n$ is, the more
of both.

```{code-block} python
:caption: smooth.py

import numpy as np
from signal_toolkit import moving_average

noisy = np.sin(np.linspace(0, 10, 500)) + np.random.normal(0, 0.2, 500)
smooth = moving_average(noisy, window=15)
```

:::{warning}
The first $n - 1$ samples of the result are computed from fewer values than
the rest. Drop them, or pass `mode="valid"`, if the start of the signal
matters.
:::

### Choosing a window

```{list-table} What a window does to a 1 kHz signal sampled at 48 kHz
:header-rows: 1

* - Window
  - Noise left
  - Edge blur
  - Use it for
* - 5 samples
  - 45%
  - 0.1 ms
  - Fast-changing signals
* - 15 samples
  - 26%
  - 0.3 ms
  - General use
* - 51 samples
  - 14%
  - 1.1 ms
  - Slow trends
```

The noise left over falls with the square root of the window:

$$
\sigma_y = \frac{\sigma_x}{\sqrt{n}}
$$ (noise)

### Other filters

```{csv-table} Filters in the toolkit
:header: "Function", "Kind", "Keeps edges"

"moving_average", "Low-pass", "No"
"median_filter", "Non-linear", "Yes"
"butterworth", "Low-pass, high-pass or band-pass", "Mostly"
```

::::{tab-set}

:::{tab-item} Median
Replaces each sample by the median of its neighbors. A single spike, however
large, disappears completely.
:::

:::{tab-item} Butterworth
A classic filter with the flattest possible response in the pass band.
Choose the order: higher is sharper, and rings more.
:::

::::

## Units

H{sub}`2`O absorbs sound differently at 20 °C than at 4 °C, so every function
that models water takes a temperature. Areas are in m{sup}`2`.

:::{admonition} A word on sample rates
:class: tip

Every function takes the sample rate in hertz as its last argument. There is
no global setting, on purpose: two signals in one program rarely share one.
:::

```{deprecated} 2.3
`smooth()` is the old name of `moving_average()` and will be removed in 3.0.
```

## Glossary

Sample rate
: How many samples of the signal there are per second.

Window
: The number of neighboring samples a filter looks at for each output sample.

See {ref}`the top of the page <top>` for the contents.
