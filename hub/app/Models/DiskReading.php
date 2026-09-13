<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

class DiskReading extends Model
{
    public $timestamps = false;

    protected $fillable = [
        'machine_id',
        'free_bytes',
        'total_bytes',
        'recorded_at',
    ];

    protected function casts(): array
    {
        return [
            'recorded_at' => 'datetime',
        ];
    }

    public function machine(): BelongsTo
    {
        return $this->belongsTo(Machine::class);
    }

    public function freePercent(): float
    {
        return $this->total_bytes > 0 ? ($this->free_bytes / $this->total_bytes) * 100 : 0.0;
    }
}
