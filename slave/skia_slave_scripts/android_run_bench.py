#!/usr/bin/env python
# Copyright (c) 2012 The Chromium Authors. All rights reserved.
# Use of this source code is governed by a BSD-style license that can be
# found in the LICENSE file.

""" Run the Skia bench executable. """

from android_build_step import AndroidBuildStep
from build_step import BuildStep
from run_bench import BenchArgs
from run_bench import PreBench
from run_bench import RunBench
from utils import android_utils
import sys


def DoBench(serial, executable, perf_data_dir, device_perf_dir, data_file,
            extra_args=None, has_root=True):
  """ Runs an Android benchmarking executable.
  
  serial: string indicating serial number of the Android device to target
  executable: string indicating which program to run
  perf_data_dir: output path for performance data
  device_perf_dir: path on the Android device where performance data will be
      temporarily stored.
  data_file: string containing the path to the perf data file on the device
  extra_args: list of any extra arguments to pass to the executable.
  has_root: boolean indicating whether we have root access to the device.
  """
  cmd_args = extra_args or []
  if perf_data_dir:
    PreBench(perf_data_dir)
    try:
      android_utils.RunADB(serial, ['shell', 'rm', '-r', device_perf_dir])
    except Exception:
      pass
    android_utils.RunADB(serial, ['shell', 'mkdir', '-p', device_perf_dir])
    cmd_args += BenchArgs(RunBench.BENCH_REPEAT_COUNT, data_file)
    android_utils.RunSkia(serial, [executable] + cmd_args,
                          use_intent=(not has_root), stop_shell=has_root)
    android_utils.RunADB(serial, ['pull', data_file, perf_data_dir])
    android_utils.RunADB(serial, ['shell', 'rm', '-r', device_perf_dir])
  else:
    android_utils.RunSkia(serial, [executable] + cmd_args,
                          use_intent=(not has_root), stop_shell=has_root)


class AndroidRunBench(RunBench, AndroidBuildStep):
  def __init__(self, timeout=4800, **kwargs):
    super(AndroidRunBench, self).__init__(timeout=timeout, **kwargs)

  def _Run(self):
    data_file = self._BuildDataFile(self._device_dirs.PerfDir())
    DoBench(serial=self._serial,
            executable='bench',
            perf_data_dir=self._perf_data_dir,
            device_perf_dir=self._device_dirs.PerfDir(),
            data_file=data_file,
            has_root=self._has_root)


if '__main__' == __name__:
  sys.exit(BuildStep.RunBuildStep(AndroidRunBench))
