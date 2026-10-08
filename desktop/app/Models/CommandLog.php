<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;

class CommandLog extends Model
{
    protected $fillable = ['scope', 'command', 'status', 'exit_code', 'output'];

    public const STATUSES = ['ok' => 'ok', 'dry-run' => 'dry-run', 'error' => 'error'];
}
