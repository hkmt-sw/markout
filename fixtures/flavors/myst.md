---
title: MyST sample
---

(my-target)=
# MyST

% a comment line

```{contents}
```

## Directives

```{note}
A backtick directive with **markup**.
```

:::{warning} Careful
A colon directive.
:::

```{admonition} Custom title
:class: tip
Body text.
```

```{code-block} python
:linenos:
print("hello")
```

```{math}
:label: eq1
a^2 + b^2 = c^2
```

```{figure} plot.png
:width: 60%
:alt: A plot
The caption.
```

```{toctree}
intro
```

## Roles

Inline {math}`x_i`, H{sub}`2`O, a {ref}`link title <my-target>` and {kbd}`Ctrl`.
