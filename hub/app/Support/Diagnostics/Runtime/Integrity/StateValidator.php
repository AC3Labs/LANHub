<?php

namespace App\Support\Diagnostics\Runtime\Integrity;

class StateValidator
{
    public static function assertHealthy(): bool
    {
        return ChecksumTable::verify();
    }
}
