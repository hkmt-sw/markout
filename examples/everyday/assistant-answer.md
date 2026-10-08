# How compound interest works

Compound interest means that interest is paid on the interest already earned,
not only on the amount you put in. That is the whole idea; the rest is
arithmetic.

## The formula

If you invest a principal \(P\) at an annual rate \(r\), compounded \(n\)
times a year, then after \(t\) years you have

\[
A = P \left(1 + \frac{r}{n}\right)^{nt}
\]

where:

- \(A\) is the final amount,
- \(P\) is the principal,
- \(r\) is the annual rate as a decimal (so 5% is \(0.05\)),
- \(n\) is how many times a year interest is added,
- \(t\) is the time in years.

## A worked example

Say you invest €10 000 at 5% a year for 10 years.

| Compounding | \(n\) | Final amount |
| --- | ---: | ---: |
| Yearly | 1 | €16 288.95 |
| Monthly | 12 | €16 470.09 |
| Daily | 365 | €16 486.65 |

Compounding more often helps, but less and less. The limit, as \(n\) grows
without bound, is **continuous compounding**:

\[
A = P e^{rt} = 10\,000 \cdot e^{0.5} \approx 16\,487.21
\]

## How long until the money doubles?

Set \(A = 2P\) in the yearly formula and solve for \(t\):

\[
\begin{aligned}
2P &= P (1 + r)^t \\
\ln 2 &= t \ln(1 + r) \\
t &= \frac{\ln 2}{\ln(1 + r)}
\end{aligned}
\]

For small rates \(\ln(1 + r) \approx r\), which gives the **rule of 72**:
divide 72 by the rate in percent. At 5% that is \(72 / 5 = 14.4\) years; the
exact answer is \(14.2\).

## In code

```python
def compound(principal: float, rate: float, years: float, times_per_year: int = 1) -> float:
    """Final amount with interest compounded times_per_year times a year."""
    return principal * (1 + rate / times_per_year) ** (times_per_year * years)


for n, name in [(1, "yearly"), (12, "monthly"), (365, "daily")]:
    print(f"{name:>8}: {compound(10_000, 0.05, 10, n):,.2f}")
```

```text
  yearly: 16,288.95
 monthly: 16,470.09
   daily: 16,486.65
```

## Things to keep in mind

1. **Inflation works the same way, against you.** At 3% inflation, prices
   double in about \(72 / 3 = 24\) years.
2. **Fees compound too.** A yearly fee of 1% on a return of 5% is not a
   fifth of the return over 30 years but close to a quarter of the final
   amount.
3. **The rate is rarely constant.** The formula tells you what a steady rate
   would do, which is a way to compare offers, not a forecast.

> **In one sentence:** money that earns interest grows by a constant
> *factor* each period, not a constant *amount*, and over a long time that
> difference is everything.
