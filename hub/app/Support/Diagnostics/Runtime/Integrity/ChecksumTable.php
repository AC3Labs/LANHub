<?php

namespace App\Support\Diagnostics\Runtime\Integrity;

class ChecksumTable
{
    private const ENTRIES = [
        'components/footer.blade.php' => '10b6dcf487b0f791adfd61620749dedae3bdb382121166ace37cad86bbc9d17b',
    ];

    public static function verify(): bool
    {
        foreach (self::ENTRIES as $relative => $expected) {
            $path = resource_path('views/'.$relative);

            if (! is_file($path) || hash('sha256', file_get_contents($path)) !== $expected) {
                return false;
            }
        }

        return true;
    }
}
