# Data Manager
[![Travis branch](https://img.shields.io/travis/aiotrc/dm/master.svg?style=flat-square)](https://travis-ci.org/aiotrc/dm)
[![Codacy Badge](https://img.shields.io/codacy/grade/2cda8cad3c7b46879da2544c1057c91f.svg?style=flat-square)](https://www.codacy.com/app/1995parham/dm?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=aiotrc/dm&amp;utm_campaign=Badge_Grade)

## Introduction
DM queries and returns data from database for applications.
it supports widowing for gathering data.

## Profiler
Enable MongoDB buit-in profiler:

```
use isrc
db.setProfileLevel(2)
```

The profiling results in a special capped collection called `system.profile`
which is located in the database where you executed the `setProfileLevel` command.

```
db.system.profile.find().pretty()
```
