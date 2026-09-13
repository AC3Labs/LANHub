<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

class SyncRule extends Model
{
    protected $fillable = [
        'name',
        'source_machine_id',
        'source_path',
        'destination_machine_id',
        'destination_path',
        'direction',
        'interval_minutes',
        'enabled',
        'last_run_at',
        'has_conflict',
        'last_error',
        'created_by',
    ];

    protected function casts(): array
    {
        return [
            'enabled' => 'boolean',
            'has_conflict' => 'boolean',
            'last_run_at' => 'datetime',
        ];
    }

    public function sourceMachine(): BelongsTo
    {
        return $this->belongsTo(Machine::class, 'source_machine_id');
    }

    public function destinationMachine(): BelongsTo
    {
        return $this->belongsTo(Machine::class, 'destination_machine_id');
    }

    public function creator(): BelongsTo
    {
        return $this->belongsTo(User::class, 'created_by');
    }

    public function isDue(): bool
    {
        return $this->enabled
            && (! $this->last_run_at || $this->last_run_at->addMinutes($this->interval_minutes)->isPast());
    }
}
